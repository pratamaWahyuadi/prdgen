package pipeline

import (
	"context"
	"fmt"
	"strings"

	"github.com/pratamaWahyuadi/prdgen/internal/llm"
	"github.com/pratamaWahyuadi/prdgen/internal/prompts"
)

type Runner struct {
	Provider  llm.Provider
	PromptDir string
}

func (r *Runner) loadPrompt(n prompts.Name) (string, error) {
	if r.PromptDir != "" {
		return prompts.LoadFromDir(r.PromptDir, n)
	}
	return prompts.Load(n)
}

// truncatedFinishReasons: daftar finish reason (lowercase) yang berarti output
// model TERPOTONG, bukan selesai secara normal. Menyimpan dokumen yang
// terpotong itu fatal: file dianggap "complete" oleh IsComplete, run berikutnya
// resume memakai dokumen setengah jadi itu sebagai konteks, dan meracuni
// semua stage downstream secara diam-diam. DeepSeek memakai "length", Gemini
// memakai "MAX_TOKENS".
var truncatedFinishReasons = map[string]bool{
	"length":     true, // OpenAI-compatible / DeepSeek
	"max_tokens": true, // Gemini
}

// IsTruncated melaporkan apakah sebuah response LLM kemungkinan besar
// terpotong di tengah karena kehabisan budget token. Dipakai di semua stage
// pipeline supaya dokumen setengah jadi tidak pernah tersimpan ke disk.
func IsTruncated(finishReason string) bool {
	return truncatedFinishReasons[strings.ToLower(strings.TrimSpace(finishReason))]
}

func (r *Runner) complete(ctx context.Context, systemPrompt, userContent string) (string, error) {
	resp, err := r.Provider.Complete(ctx, llm.CompletionRequest{
		SystemPrompt: systemPrompt,
		Messages: []llm.Message{
			{Role: "user", Content: userContent},
		},
		Temperature: 0.4,
		MaxTokens:   100000,
	})
	if err != nil {
		return "", err
	}
	if IsTruncated(resp.FinishReason) {
		return "", fmt.Errorf(
			"pipeline: output model terpotong (finish_reason=%q) -- dokumen TIDAK disimpan agar tidak meracuni stage berikutnya. "+
				"Jalankan ulang command yang sama untuk mencoba generate ulang",
			resp.FinishReason,
		)
	}
	return resp.Content, nil
}

func (r *Runner) RunDiscovery(ctx context.Context, rawIdea string) (string, error) {
	sys, err := r.loadPrompt(prompts.Discovery)
	if err != nil {
		return "", fmt.Errorf("pipeline: load discovery prompt: %w", err)
	}
	out, err := r.complete(ctx, sys, "Ide aplikasi dari user:\n\n"+rawIdea)
	if err != nil {
		return "", fmt.Errorf("pipeline: discovery stage: %w", err)
	}
	return out, nil
}

// RunDiscoveryBrief menyusun Product Brief (ringkasan terstruktur jawaban
// fase 1) SETELAH user menjawab semua pertanyaan discovery. Brief ini jadi
// konteks ringkas untuk fase deep-dive dan PRD -- menggantikan keharusan
// membaca ulang seluruh Q&A panjang.
func (r *Runner) RunDiscoveryBrief(ctx context.Context, rawIdea, discoveryQA string) (string, error) {
	sys, err := r.loadPrompt(prompts.DiscoveryBrief)
	if err != nil {
		return "", fmt.Errorf("pipeline: load discovery_brief prompt: %w", err)
	}
	userContent := fmt.Sprintf(
		"Ide aplikasi:\n%s\n\nPertanyaan discovery fase 1 beserta jawaban user:\n%s",
		rawIdea, discoveryQA,
	)
	out, err := r.complete(ctx, sys, userContent)
	if err != nil {
		return "", fmt.Errorf("pipeline: discovery_brief stage: %w", err)
	}
	return out, nil
}

// RunDiscoveryDeep menggali keputusan low-level (driver, pool, query layer,
// migration, cache, MQ, HTTP client, config, testing, CI/CD, struktur
// folder) SETELAH user melewati gate dan memilih lanjut deep-dive. Input
// menyertakan Product Brief supaya pertanyaan deep-dive membumi di konteks
// project, bukan generik.
func (r *Runner) RunDiscoveryDeep(ctx context.Context, rawIdea, productBrief string) (string, error) {
	sys, err := r.loadPrompt(prompts.DiscoveryDeep)
	if err != nil {
		return "", fmt.Errorf("pipeline: load discovery_deep prompt: %w", err)
	}
	userContent := fmt.Sprintf(
		"Ide aplikasi:\n%s\n\nProduct Brief hasil fase 1:\n%s",
		rawIdea, productBrief,
	)
	out, err := r.complete(ctx, sys, userContent)
	if err != nil {
		return "", fmt.Errorf("pipeline: discovery_deep stage: %w", err)
	}
	return out, nil
}

// RunGenerateDefaults mengisi defaults.yaml saat user SKIP Technical Deep
// Dive. Semua keputusan low-level diberi default eksplisit berbasis
// familiarity tim (bukan best-practice generik), flagged assumed:true --
// supaya asumsi terkontrol dan bisa diaudit, bukan tersembunyi di badan
// PRD/LLD.
func (r *Runner) RunGenerateDefaults(ctx context.Context, rawIdea, productBrief string) (string, error) {
	sys, err := r.loadPrompt(prompts.DefaultsGen)
	if err != nil {
		return "", fmt.Errorf("pipeline: load defaults_gen prompt: %w", err)
	}
	userContent := fmt.Sprintf(
		"Ide aplikasi:\n%s\n\nProduct Brief hasil discovery fase 1:\n%s",
		rawIdea, productBrief,
	)
	out, err := r.complete(ctx, sys, userContent)
	if err != nil {
		return "", fmt.Errorf("pipeline: defaults_gen stage: %w", err)
	}
	return out, nil
}

func (r *Runner) RunSecurity(ctx context.Context, rawIdea, discoveryQA string) (string, error) {
	sys, err := r.loadPrompt(prompts.Security)
	if err != nil {
		return "", fmt.Errorf("pipeline: load security prompt: %w", err)
	}
	userContent := fmt.Sprintf(
		"Ide aplikasi:\n%s\n\nHasil discovery (Q&A dengan user):\n%s",
		rawIdea, discoveryQA,
	)
	out, err := r.complete(ctx, sys, userContent)
	if err != nil {
		return "", fmt.Errorf("pipeline: security stage: %w", err)
	}
	return out, nil
}

// RunPRD menghasilkan PRD final. Parameter deepDiveCtx berisi hasil Technical
// Deep Dive (kalau user ikut) ATAU isi defaults.yaml (kalau user skip) --
// dua-duanya adalah sumber keputusan low-level yang HARUS dikutip PRD,
// bukan ditebak ulang.
func (r *Runner) RunPRD(ctx context.Context, rawIdea, discoveryQA, threatReport, deepDiveCtx string) (string, error) {
	sys, err := r.loadPrompt(prompts.PRD)
	if err != nil {
		return "", fmt.Errorf("pipeline: load prd prompt: %w", err)
	}
	deepSection := ""
	if strings.TrimSpace(deepDiveCtx) != "" {
		deepSection = "\n\n---\nHASIL DEEP-DIVE / DEFAULT EKSPLISIT (keputusan low-level -- sumber WAJIB dikutip di section Asumsi Teknis, jangan ditebak ulang):\n" + deepDiveCtx + "\n"
	}
	userContent := fmt.Sprintf(
		"Ide aplikasi:\n%s\n\nHasil discovery (Q&A dengan user):\n%s\n\nThreat report dari Security Auditor:\n%s%s\n\n"+
			"---\nPENGINGAT PENTING SEBELUM MENULIS PRD:\n"+
			"Semua jawaban user di hasil discovery di atas adalah KEPUTUSAN FINAL, "+
			"bukan saran yang boleh diganti. Sebelum menulis section 7 (Tech Stack), "+
			"baca ulang jawaban discovery yang menyebutkan bahasa, database, ORM/query "+
			"approach, frontend, auth, dan deployment -- pastikan section itu PERSIS "+
			"mengikuti jawaban tersebut, bukan pilihanmu sendiri. Kalau user bilang "+
			"\"bebas\"/\"terserah\" untuk sesuatu, tandai keputusanmu dengan "+
			"\"🔶 Asumsi (belum dikonfirmasi user)\".\n\nJawaban discovery (diulang untuk referensi):\n%s",
		rawIdea, discoveryQA, threatReport, deepSection, discoveryQA,
	)
	out, err := r.complete(ctx, sys, userContent)
	if err != nil {
		return "", fmt.Errorf("pipeline: prd stage: %w", err)
	}
	return out, nil
}

func (r *Runner) RunLLDErd(ctx context.Context, prd string) (string, error) {
	sys, err := r.loadPrompt(prompts.LLDErd)
	if err != nil {
		return "", fmt.Errorf("pipeline: load lld_erd prompt: %w", err)
	}
	out, err := r.complete(ctx, sys, "PRD & Architecture Blueprint:\n\n"+prd)
	if err != nil {
		return "", fmt.Errorf("pipeline: lld erd stage: %w", err)
	}
	return out, nil
}

func (r *Runner) RunLLDApi(ctx context.Context, prd, schema string) (string, error) {
	sys, err := r.loadPrompt(prompts.LLDApi)
	if err != nil {
		return "", fmt.Errorf("pipeline: load lld_api prompt: %w", err)
	}
	userContent := fmt.Sprintf(
		"PRD & Architecture Blueprint:\n%s\n\nDatabase Schema/ERD:\n%s",
		prd, schema,
	)
	out, err := r.complete(ctx, sys, userContent)
	if err != nil {
		return "", fmt.Errorf("pipeline: lld api stage: %w", err)
	}
	return out, nil
}

func (r *Runner) RunLLDPlan(ctx context.Context, prd, schema, apiContracts string) (string, error) {
	sys, err := r.loadPrompt(prompts.LLDPlan)
	if err != nil {
		return "", fmt.Errorf("pipeline: load lld_plan prompt: %w", err)
	}
	userContent := fmt.Sprintf(
		"PRD & Architecture Blueprint:\n%s\n\nDatabase Schema/ERD:\n%s\n\nAPI Contracts:\n%s\n\n"+
			"---\nPENGINGAT PENTING: sebelum menulis coding plan, cek ulang section "+
			"'Tech Stack & Justifikasi' di PRD di atas. Bahasa, framework, database, "+
			"dan pendekatan akses data (ORM/raw SQL/code-gen) di coding plan ini WAJIB "+
			"identik dengan yang PRD putuskan -- JANGAN mengganti dengan stack lain "+
			"walau kamu punya preferensi sendiri.",
		prd, schema, apiContracts,
	)
	out, err := r.complete(ctx, sys, userContent)
	if err != nil {
		return "", fmt.Errorf("pipeline: lld plan stage: %w", err)
	}
	return out, nil
}

// RunValidatePRD membandingkan draft PRD dengan seluruh hasil discovery --
// fase 1 + fase 2 deep-dive (kalau ada) + defaults.yaml (kalau user skip) --
// supaya validator bisa menilai coverage jawaban dan ketepatan flag
// [ASSUMED], bukan cuma konsistensi fase 1.
func (r *Runner) RunValidatePRD(ctx context.Context, discoveryCtx, prd string) (string, error) {
	sys, err := r.loadPrompt(prompts.PRDConsistency)
	if err != nil {
		return "", fmt.Errorf("pipeline: load prd_consistency prompt: %w", err)
	}
	userContent := fmt.Sprintf(
		"Hasil discovery lengkap (Q&A dengan user, termasuk deep-dive/defaults kalau ada):\n%s\n\nDraft PRD:\n%s",
		discoveryCtx, prd,
	)
	out, err := r.complete(ctx, sys, userContent)
	if err != nil {
		return "", fmt.Errorf("pipeline: validate prd stage: %w", err)
	}
	return out, nil
}

func (r *Runner) RunValidateLLD(ctx context.Context, prd, schema, apiContracts, codingPlan string) (string, error) {
	sys, err := r.loadPrompt(prompts.LLDConsistency)
	if err != nil {
		return "", fmt.Errorf("pipeline: load lld_consistency prompt: %w", err)
	}
	userContent := fmt.Sprintf(
		"PRD (perhatikan section Tech Stack & Justifikasi):\n%s\n\nDatabase Schema:\n%s\n\nAPI Contracts:\n%s\n\nCoding Plan:\n%s",
		prd, schema, apiContracts, codingPlan,
	)
	out, err := r.complete(ctx, sys, userContent)
	if err != nil {
		return "", fmt.Errorf("pipeline: validate lld stage: %w", err)
	}
	return out, nil
}

func (r *Runner) RunGenerateIssues(ctx context.Context, prd, schema, apiContracts, codingPlan string) (string, error) {
	sys, err := r.loadPrompt(prompts.GHIssues)
	if err != nil {
		return "", fmt.Errorf("pipeline: load gh_issues prompt: %w", err)
	}
	userContent := fmt.Sprintf(
		"PRD:\n%s\n\nDatabase Schema:\n%s\n\nAPI Contracts:\n%s\n\nCoding Plan:\n%s\n\n"+
			"---\nPENGINGAT PENTING: output HARUS JSON array murni, tanpa markdown code "+
			"fence, tanpa teks lain di luar JSON.",
		prd, schema, apiContracts, codingPlan,
	)
	out, err := r.complete(ctx, sys, userContent)
	if err != nil {
		return "", fmt.Errorf("pipeline: generate issues stage: %w", err)
	}
	return out, nil
}

func (r *Runner) RunReviseIssue(ctx context.Context, schema, apiContracts, codingPlan, currentIssueJSON, feedback string) (string, error) {
	sys, err := r.loadPrompt(prompts.GHIssueRevise)
	if err != nil {
		return "", fmt.Errorf("pipeline: load gh_issue_revise prompt: %w", err)
	}
	userContent := fmt.Sprintf(
		"Draft issue saat ini:\n%s\n\nFeedback dari user:\n%s\n\n"+
			"Database Schema:\n%s\n\nAPI Contracts:\n%s\n\nCoding Plan:\n%s\n\n"+
			"---\nPENGINGAT PENTING: output HARUS satu object JSON murni, BUKAN array, "+
			"tanpa markdown code fence.",
		currentIssueJSON, feedback, schema, apiContracts, codingPlan,
	)
	out, err := r.complete(ctx, sys, userContent)
	if err != nil {
		return "", fmt.Errorf("pipeline: revise issue stage: %w", err)
	}
	return out, nil
}

// RunReviseDocument merevisi SATU dokumen penuh (PRD, schema, API contracts,
// atau coding plan) berdasarkan feedback bebas dari user. Dipakai oleh
// command `prdgen revise` -- solusi generik untuk masalah yang sama dengan
// gh_issue_revise: validator (RunValidatePRD/RunValidateLLD) cuma melaporkan
// masalah, tidak pernah benar-benar memperbaikinya. Ini fungsi yang benar-
// benar memperbaiki, dipicu manual oleh user, bukan otomatis.
func (r *Runner) RunReviseDocument(ctx context.Context, currentDoc, feedback, docContext string) (string, error) {
	sys, err := r.loadPrompt(prompts.DocumentRevise)
	if err != nil {
		return "", fmt.Errorf("pipeline: load document_revise prompt: %w", err)
	}
	userContent := fmt.Sprintf(
		"Dokumen saat ini:\n%s\n\nFeedback dari user:\n%s\n\nDokumen konteks pendukung:\n%s\n\n"+
			"---\nPENGINGAT PENTING: output HARUS dokumen lengkap versi revisi, tanpa "+
			"komentar atau penjelasan apapun di luar dokumen, karena hasil ini akan "+
			"langsung menimpa file dokumen asli.",
		currentDoc, feedback, docContext,
	)
	out, err := r.complete(ctx, sys, userContent)
	if err != nil {
		return "", fmt.Errorf("pipeline: revise document stage: %w", err)
	}
	return out, nil
}
