package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"prdgen/internal/ghissues"
	"prdgen/internal/llm"
	"prdgen/internal/pipeline"
	"prdgen/internal/store"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	// Flag global sederhana: --yes/-y bisa muncul di posisi mana saja dalam
	// args (bukan cuma di akhir), jadi dipisahkan dulu sebelum parsing
	// argumen posisional (cmd, projectDir, extraArg) supaya urutan
	// penulisan command tetap fleksibel, misal:
	//   prdgen issues ./proj owner/repo --yes
	//   prdgen issues ./proj --yes owner/repo
	args, autoConfirm := extractYesFlag(args)

	if len(args) < 2 {
		printUsage()
		return fmt.Errorf("argumen tidak lengkap")
	}

	cmd, projectDir := args[0], args[1]
	var extraArg string
	if len(args) >= 3 {
		extraArg = args[2]
	}

	loadDotEnv(filepath.Join(projectDir, ".env"))
	loadDotEnv(".env")

	promptDir := os.Getenv("PRDGEN_PROMPT_DIR")

	provider, err := buildProvider()
	if err != nil {
		return err
	}
	runner := &pipeline.Runner{Provider: provider, PromptDir: promptDir}

	s, err := store.New(projectDir)
	if err != nil {
		return err
	}

	ctx := context.Background()
	reader := bufio.NewReader(os.Stdin)

	switch cmd {
	case "new":
		return runPRDPipeline(ctx, runner, s, reader)
	case "lld":
		return runLLDPipeline(ctx, runner, s, reader)
	case "issues":
		return runIssuesPipeline(ctx, runner, s, reader, extraArg, autoConfirm)
	case "revise":
		return runRevisePipeline(ctx, runner, s, reader, extraArg)
	default:
		printUsage()
		return fmt.Errorf("perintah tidak dikenal: %s", cmd)
	}
}

// buildProvider memilih provider LLM berdasarkan env var LLM_PROVIDER
// ("deepseek" default, atau "gemini"). Ditaruh di satu tempat supaya
// nambah provider baru di masa depan cukup nambah satu case lagi di sini
// tanpa nyentuh logic pipeline sama sekali (lihat internal/llm.Provider).
func buildProvider() (llm.Provider, error) {
	name := strings.ToLower(strings.TrimSpace(os.Getenv("LLM_PROVIDER")))
	if name == "" {
		name = "deepseek"
	}

	switch name {
	case "deepseek":
		apiKey := os.Getenv("DEEPSEEK_API_KEY")
		if apiKey == "" {
			return nil, fmt.Errorf("env DEEPSEEK_API_KEY belum di-set")
		}
		model := os.Getenv("DEEPSEEK_MODEL")
		if model == "" {
			model = "deepseek-chat"
		}
		return llm.NewDeepSeekProvider(apiKey, model), nil

	case "gemini":
		apiKey := os.Getenv("GEMINI_API_KEY")
		if apiKey == "" {
			return nil, fmt.Errorf("env GEMINI_API_KEY belum di-set")
		}
		model := os.Getenv("GEMINI_MODEL")
		if model == "" {
			model = "gemini-flash-latest"
		}
		return llm.NewGeminiProvider(apiKey, model), nil

	default:
		return nil, fmt.Errorf("LLM_PROVIDER tidak dikenal: %q (pilihan: deepseek, gemini)", name)
	}
}

func printUsage() {
	fmt.Println(`Pemakaian:
  prdgen new <project-dir>                  discovery -> security audit -> PRD -> validasi PRD
  prdgen lld <project-dir>                  ERD -> API contracts -> coding plan -> validasi LLD
  prdgen issues <project-dir> [owner/repo] [--yes|-y]  generate GitHub issues dari LLD_PLAN.md
  prdgen revise <project-dir> [prd|schema|api|plan]  revisi dokumen berdasarkan feedback kamu

Perintah 'issues' butuh binary 'gh' (GitHub CLI) sudah terinstall dan login
('gh auth login'). Setiap issue ditampilkan dulu untuk direview sebelum
benar-benar dibuat -- LLM tidak pernah menjalankan command apapun, cuma
menghasilkan data (title/body/labels) yang dieksekusi oleh kode Go.

Tambahkan '--yes' (atau '-y') untuk skip review satu-per-satu dan langsung
buat SEMUA issue dari draft (ISSUES.json) ke GitHub tanpa konfirmasi.
Aman dipakai kalau draft-nya sudah lo baca/setujui duluan, karena flag ini
cuma mempercepat eksekusi 'gh issue create' per issue -- tidak memanggil
LLM sama sekali (draft sudah final, tidak ada yang di-generate ulang).
Issue yang sudah pernah dibuat sebelumnya (tercatat di ISSUES_CREATED.log)
tetap otomatis dilewati seperti biasa.

Perintah 'revise' dipakai kalau PRD/schema/API contracts/coding plan yang
sudah di-generate ada bagian yang salah atau kurang pas -- kasih feedback
bebas, dokumen direvisi, kamu review hasilnya dulu sebelum ditimpa ke file.

Env vars:
  LLM_PROVIDER        (opsional, default "deepseek") pilih "deepseek" atau "gemini"
  DEEPSEEK_API_KEY    (wajib kalau LLM_PROVIDER=deepseek) API key DeepSeek
  DEEPSEEK_MODEL      (opsional, default "deepseek-chat")
  GEMINI_API_KEY      (wajib kalau LLM_PROVIDER=gemini) API key Gemini (Google AI Studio)
  GEMINI_MODEL        (opsional, default "gemini-flash-latest")
  PRDGEN_PROMPT_DIR   (opsional) folder berisi *.txt prompt custom, override default`)
}

// extractYesFlag memisahkan flag --yes/-y dari argumen posisional lain,
// supaya bisa ditulis di posisi mana saja tanpa mengacaukan parsing
// cmd/projectDir/extraArg di run().
func extractYesFlag(args []string) ([]string, bool) {
	out := make([]string, 0, len(args))
	found := false
	for _, a := range args {
		if a == "--yes" || a == "-y" {
			found = true
			continue
		}
		out = append(out, a)
	}
	return out, found
}

func runPRDPipeline(ctx context.Context, r *pipeline.Runner, s *store.Store, reader *bufio.Reader) error {
	fmt.Println("== prdgen: PRD pipeline ==")

	var rawIdea, discoveryQA, threatReport, prd string
	stage := determineStartStage(s)

	if stage != pipeline.StageDiscovery {
		fmt.Printf("Ditemukan checkpoint sebelumnya, resume dari stage: %s\n", stage)
	}

	if s.IsComplete(store.FileIdea) {
		v, err := s.Load(store.FileIdea)
		if err != nil {
			return err
		}
		rawIdea = v
	}
	if s.IsComplete(store.FileDiscoveryQA) {
		v, err := s.Load(store.FileDiscoveryQA)
		if err != nil {
			return err
		}
		discoveryQA = v
	}
	if s.IsComplete(store.FileThreatReport) {
		v, err := s.Load(store.FileThreatReport)
		if err != nil {
			return err
		}
		threatReport = v
	}

	if stage == pipeline.StageDiscovery {
		if s.IsComplete(store.FileIdea) {
			// 00_idea.md sudah ada dari sesi sebelumnya (rawIdea sudah
			// di-load di atas). Jangan tanya lagi -- ini bug lama: sebelum
			// fix ini, kondisinya cuma cek stage == StageDiscovery tanpa
			// cek apakah ide-nya sendiri sudah tersimpan, jadi user selalu
			// diminta nulis ide ulang tiap kali resume ke stage discovery
			// (misal karena baru selesai jawab 01a_discovery_questions.md
			// secara manual tapi belum sempat generate 01_discovery_qa.md).
			fmt.Println("\n[idea] ditemukan 00_idea.md dari sesi sebelumnya, lanjut pakai itu.")
		} else {
			fmt.Println("sebelum isi ide disini brainstorming dulu dengan ai di web yang gratis,lalu gambar di excalidraw untuk visualisasi dan setelah konsepnya matang baru ke sini")
			fmt.Println("Tulis ide aplikasi kamu (akhiri dengan baris berisi EOF lalu Enter, atau Ctrl+D):")
			rawIdea = readMultiline(reader)
			if strings.TrimSpace(rawIdea) == "" {
				return fmt.Errorf("ide tidak boleh kosong")
			}
			if _, err := s.Save(store.FileIdea, rawIdea); err != nil {
				return err
			}
		}
	}

	for stage != pipeline.StageDone {
		switch stage {
		case pipeline.StageDiscovery:
			var questions string
			if s.IsComplete(store.FileDiscoveryQuestions) {
				// Sudah pernah generate pertanyaan di run sebelumnya (misal
				// user Ctrl+C sebelum sempat selesai jawab). Pakai yang
				// tersimpan, JANGAN panggil LLM lagi -- hemat biaya & tetap
				// konsisten pertanyaannya sama seperti yang sudah dilihat.
				fmt.Println("\n[discovery] ditemukan pertanyaan dari sesi sebelumnya, lanjut dari situ.")
				v, err := s.Load(store.FileDiscoveryQuestions)
				if err != nil {
					return err
				}
				questions = v
			} else {
				fmt.Println("\n[discovery] menghubungi model...")
				v, err := r.RunDiscovery(ctx, rawIdea)
				if err != nil {
					return fmt.Errorf("stage discovery: %w", err)
				}
				questions = v
				if _, err := s.Save(store.FileDiscoveryQuestions, questions); err != nil {
					return err
				}
			}

			fmt.Println("\n--- Pertanyaan Discovery ---")
			fmt.Println(questions)
			fmt.Println("\nJawab semua pertanyaan di atas (akhiri dengan baris berisi EOF lalu Enter, atau Ctrl+D):")
			answers := readMultiline(reader)
			discoveryQA = questions + "\n\n=== Jawaban User ===\n" + answers
			if _, err := s.Save(store.FileDiscoveryQA, discoveryQA); err != nil {
				return err
			}

		case pipeline.StageSecurity:
			fmt.Println("\n[security] menjalankan threat modeling...")
			report, err := r.RunSecurity(ctx, rawIdea, discoveryQA)
			if err != nil {
				return fmt.Errorf("stage security: %w", err)
			}
			threatReport = report
			path, err := s.Save(store.FileThreatReport, threatReport)
			if err != nil {
				return err
			}
			fmt.Println("\n--- Threat Report ---")
			fmt.Println(threatReport)
			fmt.Printf("\nTersimpan di %s\n", path)
			if !confirm(reader, "Lanjut ke generate PRD?") {
				return fmt.Errorf("dibatalkan oleh user di stage security")
			}

		case pipeline.StagePRD:
			fmt.Println("\n[prd] menggenerate PRD final...")
			doc, err := r.RunPRD(ctx, rawIdea, discoveryQA, threatReport)
			if err != nil {
				return fmt.Errorf("stage prd: %w", err)
			}
			prd = doc
			path, err := s.Save(store.FilePRD, prd)
			if err != nil {
				return err
			}
			fmt.Printf("\n✅ PRD selesai, tersimpan di %s\n", path)
			fmt.Println("Lanjutkan dengan: prdgen lld <project-dir>")

		case pipeline.StageValidatePRD:
			fmt.Println("\n[validate] mengecek konsistensi PRD vs hasil discovery...")
			report, err := r.RunValidatePRD(ctx, discoveryQA, prd)
			if err != nil {
				fmt.Printf("⚠️  Validator gagal jalan (%v), tapi PRD tetap tersimpan.\n", err)
				break
			}
			path, err := s.Save(store.FilePRDValidation, report)
			if err != nil {
				return err
			}
			fmt.Println("\n--- Hasil Validasi PRD vs Discovery ---")
			fmt.Println(report)
			fmt.Printf("\nTersimpan di %s\n", path)
		}
		stage = stage.Next()
	}
	return nil
}

func determineStartStage(s *store.Store) pipeline.Stage {
	if !s.IsComplete(store.FileDiscoveryQA) {
		return pipeline.StageDiscovery
	}
	if !s.IsComplete(store.FileThreatReport) {
		return pipeline.StageSecurity
	}
	if !s.IsComplete(store.FilePRD) {
		return pipeline.StagePRD
	}
	if !s.IsComplete(store.FilePRDValidation) {
		return pipeline.StageValidatePRD
	}
	return pipeline.StageDone
}

// determineLLDStartStage menentukan stage mulai untuk `prdgen lld`
// berdasarkan file mana yang sudah ada, sama seperti determineStartStage
// untuk `prdgen new`. Sebelum ini ada, runLLDPipeline SELALU mulai dari
// LLDStageErd apapun kondisinya -- kalau proses mati di tengah (misal
// sudah lolos ERD+API tapi belum plan), jalan ulang bakal generate ulang
// ERD & API dari nol (buang biaya API call, dan LLM bisa menghasilkan
// schema/API yang beda dari yang sudah di-approve sebelumnya, padahal
// stage berikutnya seperti plan/issues sudah dibuat berdasarkan versi
// lama itu).
func determineLLDStartStage(s *store.Store) pipeline.LLDStage {
	if !s.IsComplete(store.FileSchema) {
		return pipeline.LLDStageErd
	}
	if !s.IsComplete(store.FileAPIContracts) {
		return pipeline.LLDStageApi
	}
	if !s.IsComplete(store.FileCodingPlan) {
		return pipeline.LLDStagePlan
	}
	if !s.IsComplete(store.FileLLDValidation) {
		return pipeline.LLDStageValidate
	}
	return pipeline.LLDStageDone
}

func runLLDPipeline(ctx context.Context, r *pipeline.Runner, s *store.Store, reader *bufio.Reader) error {
	if !s.IsComplete(store.FilePRD) {
		return fmt.Errorf("%s tidak ditemukan atau kosong, jalankan 'prdgen new <project-dir>' dulu", store.FilePRD)
	}
	prd, err := s.Load(store.FilePRD)
	if err != nil {
		return err
	}

	fmt.Println("== prdgen: LLD pipeline ==")

	stage := determineLLDStartStage(s)
	if stage != pipeline.LLDStageErd {
		fmt.Printf("Ditemukan checkpoint sebelumnya, resume dari stage: %s\n", stage)
	}

	var schema, apiContracts, codingPlan string
	if s.IsComplete(store.FileSchema) {
		v, err := s.Load(store.FileSchema)
		if err != nil {
			return err
		}
		schema = v
	}
	if s.IsComplete(store.FileAPIContracts) {
		v, err := s.Load(store.FileAPIContracts)
		if err != nil {
			return err
		}
		apiContracts = v
	}
	if s.IsComplete(store.FileCodingPlan) {
		v, err := s.Load(store.FileCodingPlan)
		if err != nil {
			return err
		}
		codingPlan = v
	}

	if stage == pipeline.LLDStageDone {
		fmt.Println("🎉 Semua tahap LLD sudah selesai sebelumnya. Hapus file terkait di project dir kalau mau generate ulang salah satu stage, atau pakai 'prdgen revise'.")
		return nil
	}

	for stage != pipeline.LLDStageDone {
		switch stage {
		case pipeline.LLDStageErd:
			fmt.Println("\n[erd] menggenerate ERD & database schema...")
			out, err := r.RunLLDErd(ctx, prd)
			if err != nil {
				return fmt.Errorf("stage erd: %w", err)
			}
			schema = out
			path, err := s.Save(store.FileSchema, schema)
			if err != nil {
				return err
			}
			fmt.Printf("✅ Schema tersimpan di %s\n", path)
			if !confirm(reader, "Lanjut ke API contracts?") {
				return fmt.Errorf("dibatalkan oleh user di stage erd")
			}

		case pipeline.LLDStageApi:
			fmt.Println("\n[api] menggenerate API contracts...")
			out, err := r.RunLLDApi(ctx, prd, schema)
			if err != nil {
				return fmt.Errorf("stage api: %w", err)
			}
			apiContracts = out
			path, err := s.Save(store.FileAPIContracts, apiContracts)
			if err != nil {
				return err
			}
			fmt.Printf("✅ API contracts tersimpan di %s\n", path)
			if !confirm(reader, "Lanjut ke coding plan?") {
				return fmt.Errorf("dibatalkan oleh user di stage api")
			}

		case pipeline.LLDStagePlan:
			fmt.Println("\n[plan] menggenerate step-by-step coding plan...")
			out, err := r.RunLLDPlan(ctx, prd, schema, apiContracts)
			if err != nil {
				return fmt.Errorf("stage plan: %w", err)
			}
			codingPlan = out
			path, err := s.Save(store.FileCodingPlan, codingPlan)
			if err != nil {
				return err
			}
			fmt.Printf("\n✅ Coding plan selesai, tersimpan di %s\n", path)
			if !confirm(reader, "Lanjut ke validasi LLD?") {
				return fmt.Errorf("dibatalkan oleh user di stage plan")
			}

		case pipeline.LLDStageValidate:
			fmt.Println("\n[validate] mengecek konsistensi LLD vs tech stack PRD...")
			report, err := r.RunValidateLLD(ctx, prd, schema, apiContracts, codingPlan)
			if err != nil {
				fmt.Printf("⚠️  Validator gagal jalan (%v), tapi LLD tetap tersimpan.\n", err)
				break
			}
			path, err := s.Save(store.FileLLDValidation, report)
			if err != nil {
				return err
			}
			fmt.Println("\n--- Hasil Validasi LLD vs Tech Stack PRD ---")
			fmt.Println(report)
			fmt.Printf("\nTersimpan di %s\n", path)
			fmt.Println("🎉 Selesai. Cek semua file .md di project dir kamu.")
		}
		stage = stage.Next()
	}
	return nil
}

// runIssuesPipeline membaca PRD + LLD yang sudah ada, generate draft issues
// (LLM, murni data JSON), lalu untuk tiap issue: tampilkan ke user, minta
// konfirmasi Enter, baru benar-benar dibuat di GitHub lewat gh CLI. LLM
// tidak pernah menyentuh terminal -- lihat internal/ghissues untuk detail
// keamanan eksekusinya.
func runIssuesPipeline(ctx context.Context, r *pipeline.Runner, s *store.Store, reader *bufio.Reader, repoOverride string, autoConfirm bool) error {
	if !s.IsComplete(store.FileCodingPlan) {
		return fmt.Errorf("%s tidak ditemukan atau kosong, jalankan 'prdgen lld <project-dir>' dulu", store.FileCodingPlan)
	}
	prd, err := s.Load(store.FilePRD)
	if err != nil {
		return err
	}
	schema, err := s.Load(store.FileSchema)
	if err != nil {
		return err
	}
	apiContracts, err := s.Load(store.FileAPIContracts)
	if err != nil {
		return err
	}
	codingPlan, err := s.Load(store.FileCodingPlan)
	if err != nil {
		return err
	}

	fmt.Println("== prdgen: GitHub Issues pipeline ==")

	var issuesJSON string
	if s.IsComplete(store.FileIssuesJSON) {
		fmt.Printf("Ditemukan %s dari run sebelumnya, pakai itu. (Hapus filenya kalau mau regenerate draft.)\n", store.FileIssuesJSON)
		issuesJSON, err = s.Load(store.FileIssuesJSON)
		if err != nil {
			return err
		}
	} else {
		fmt.Println("\n[issues] menggenerate draft GitHub issues dari coding plan...")
		out, err := r.RunGenerateIssues(ctx, prd, schema, apiContracts, codingPlan)
		if err != nil {
			return fmt.Errorf("stage generate issues: %w", err)
		}
		issuesJSON = out
		path, err := s.Save(store.FileIssuesJSON, issuesJSON)
		if err != nil {
			return err
		}
		fmt.Printf("✅ Draft issues tersimpan di %s\n", path)
	}

	issues, err := ghissues.Parse(issuesJSON)
	if err != nil {
		return fmt.Errorf("gagal parse %s: %w", store.FileIssuesJSON, err)
	}

	alreadyCreated, err := loadCreatedIssueTitles(s)
	if err != nil {
		return err
	}

	fmt.Printf("\nDitemukan %d issue di draft:\n", len(issues))
	for i, iss := range issues {
		marker := " "
		if alreadyCreated[iss.Title] {
			marker = "v"
		}
		fmt.Printf("  [%s] %d. [%s] %s\n", marker, i+1, iss.Phase, iss.Title)
	}

	if err := ghissues.CheckGHAvailable(ctx); err != nil {
		return fmt.Errorf("gh CLI belum siap: %w", err)
	}

	executor := &ghissues.GHCLIExecutor{Repo: repoOverride}

	fmt.Println("\n[issues] memastikan semua label (mis. phase-1, phase-2, ...) sudah ada di repo...")
	allLabels := collectUniqueLabels(issues)
	if err := executor.EnsureLabels(ctx, allLabels); err != nil {
		return fmt.Errorf("gagal menyiapkan label di repo: %w", err)
	}

	if autoConfirm {
		fmt.Println("\nMode --yes aktif: semua issue di draft langsung dibuat tanpa review satu-satu.")
	} else {
		fmt.Println("\nSetiap issue ditampilkan dulu sebelum dibuat.")
		fmt.Println("[Enter]=buat, [s]=skip issue ini, [q]=berhenti sekarang")
	}

	created := 0
	for i := range issues {
		iss := issues[i]
		if alreadyCreated[iss.Title] {
			fmt.Printf("\n--- Issue %d/%d: %q sudah pernah dibuat sebelumnya, skip otomatis ---\n", i+1, len(issues), iss.Title)
			continue
		}

		if autoConfirm {
			// Mode --yes: tidak ada review satu-satu, draft di ISSUES.json
			// dianggap final (sudah dibaca/disetujui user sebelumnya).
			// Tetap lewat EnsureLabels + CreateIssue yang sama seperti
			// jalur interaktif -- tidak ada logic baru yang dilewati,
			// cuma bagian tanya-jawabnya yang di-skip.
			fmt.Printf("\n--- Issue %d/%d ---\n", i+1, len(issues))
			fmt.Printf("Title : %s\n", iss.Title)
			if err := executor.EnsureLabels(ctx, iss.Labels); err != nil {
				return fmt.Errorf("gagal menyiapkan label untuk issue %q: %w", iss.Title, err)
			}
			out, err := executor.CreateIssue(ctx, iss)
			if err != nil {
				fmt.Printf("⚠️  Gagal membuat issue: %v\n", err)
				continue
			}
			fmt.Printf("✅ Dibuat: %s\n", out)
			if logErr := s.Append(store.FileIssuesCreatedLog, iss.Title+"\t"+out); logErr != nil {
				fmt.Printf("⚠️  Gagal mencatat ke log (issue tetap dibuat di GitHub): %v\n", logErr)
			}
			created++
			continue
		}

	reviewLoop:
		for {
			fmt.Printf("\n--- Issue %d/%d ---\n", i+1, len(issues))
			fmt.Printf("Title : %s\n", iss.Title)
			fmt.Printf("Phase : %s\n", iss.Phase)
			fmt.Printf("Labels: %s\n", strings.Join(iss.Labels, ", "))
			fmt.Printf("Body  :\n%s\n", iss.Body)
			fmt.Print("\n[Enter=buat, e=revisi dengan feedback, s=skip, q=berhenti]: ")

			line, _ := reader.ReadString('\n')
			choice := strings.ToLower(strings.TrimSpace(line))

			switch choice {
			case "q":
				fmt.Printf("\nDihentikan oleh user. %d/%d issue dibuat pada sesi ini.\n", created, len(issues))
				return nil

			case "s":
				fmt.Println("Dilewati.")
				break reviewLoop

			case "e":
				fmt.Print("Feedback buat issue ini: ")
				fbLine, _ := reader.ReadString('\n')
				feedback := strings.TrimSpace(fbLine)
				if feedback == "" {
					fmt.Println("Feedback kosong, dibatalkan.")
					continue reviewLoop
				}

				currentJSON, err := json.Marshal(iss)
				if err != nil {
					fmt.Printf("⚠️  Gagal menyiapkan draft untuk revisi: %v\n", err)
					continue reviewLoop
				}

				fmt.Println("[revise] mengirim feedback ke model...")
				out, err := r.RunReviseIssue(ctx, schema, apiContracts, codingPlan, string(currentJSON), feedback)
				if err != nil {
					fmt.Printf("⚠️  Revisi gagal: %v\n", err)
					continue reviewLoop
				}

				revised, err := ghissues.ParseSingle(out)
				if err != nil {
					fmt.Printf("⚠️  Gagal parse hasil revisi: %v\n", err)
					continue reviewLoop
				}

				iss = revised
				issues[i] = revised

				if data, err := json.MarshalIndent(issues, "", "  "); err == nil {
					if _, err := s.Save(store.FileIssuesJSON, string(data)); err != nil {
						fmt.Printf("⚠️  Gagal menyimpan draft revisi ke disk: %v\n", err)
					}
				}
				fmt.Println("✅ Draft direvisi, ini versi barunya:")
				continue reviewLoop

			default:
				// Revisi ('e') bisa saja menghasilkan label baru yang belum
				// ada di repo, jadi pastikan lagi tepat sebelum create.
				if err := executor.EnsureLabels(ctx, iss.Labels); err != nil {
					fmt.Printf("⚠️  Gagal menyiapkan label: %v\n", err)
					break reviewLoop
				}
				out, err := executor.CreateIssue(ctx, iss)
				if err != nil {
					fmt.Printf("⚠️  Gagal membuat issue: %v\n", err)
					break reviewLoop
				}
				fmt.Printf("✅ Dibuat: %s\n", out)
				if logErr := s.Append(store.FileIssuesCreatedLog, iss.Title+"\t"+out); logErr != nil {
					fmt.Printf("⚠️  Gagal mencatat ke log (issue tetap dibuat di GitHub): %v\n", logErr)
				}
				created++
				break reviewLoop
			}
		}
	}

	fmt.Printf("\n🎉 Selesai. %d/%d issue dibuat pada sesi ini.\n", created, len(issues))
	return nil
}

// collectUniqueLabels mengumpulkan semua label unik dari daftar issue draft,
// dipakai supaya EnsureLabels cukup dipanggil sekali di awal (satu kali
// panggilan `gh label list` + hanya `gh label create` untuk yang benar-benar
// belum ada) daripada mengecek berulang-ulang per issue.
func collectUniqueLabels(issues []ghissues.Issue) []string {
	seen := map[string]bool{}
	var out []string
	for _, iss := range issues {
		for _, l := range iss.Labels {
			l = strings.TrimSpace(l)
			if l == "" || seen[l] {
				continue
			}
			seen[l] = true
			out = append(out, l)
		}
	}
	return out
}

func loadCreatedIssueTitles(s *store.Store) (map[string]bool, error) {
	titles := map[string]bool{}
	if !s.Exists(store.FileIssuesCreatedLog) {
		return titles, nil
	}
	content, err := s.Load(store.FileIssuesCreatedLog)
	if err != nil {
		return nil, err
	}
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		title, _, _ := strings.Cut(line, "\t")
		titles[title] = true
	}
	return titles, nil
}

// runRevisePipeline merevisi SATU dokumen (PRD/schema/API contracts/coding
// plan) berdasarkan feedback bebas dari user. Ini jawaban untuk gap yang
// sama seperti di runIssuesPipeline: RunValidatePRD/RunValidateLLD cuma
// melaporkan masalah, tidak pernah memperbaikinya -- command ini yang
// benar-benar memperbaiki, dipicu manual, hasilnya direview dulu sebelum
// menimpa file asli.
func runRevisePipeline(ctx context.Context, r *pipeline.Runner, s *store.Store, reader *bufio.Reader, target string) error {
	type targetInfo struct {
		file string
	}
	targets := map[string]targetInfo{
		"prd":    {store.FilePRD},
		"schema": {store.FileSchema},
		"api":    {store.FileAPIContracts},
		"plan":   {store.FileCodingPlan},
	}

	if target == "" {
		fmt.Println("Dokumen mana yang mau direvisi? (prd/schema/api/plan)")
		fmt.Print("> ")
		line, _ := reader.ReadString('\n')
		target = strings.ToLower(strings.TrimSpace(line))
	}

	t, ok := targets[target]
	if !ok {
		return fmt.Errorf("target tidak dikenal: %q (pilihan: prd, schema, api, plan)", target)
	}
	if !s.IsComplete(t.file) {
		return fmt.Errorf("%s belum ada atau kosong, generate dulu sebelum direvisi", t.file)
	}

	currentDoc, err := s.Load(t.file)
	if err != nil {
		return err
	}

	// Kumpulkan dokumen konteks pendukung sesuai target, supaya revisi tetap
	// konsisten dengan keputusan yang sudah dibuat di dokumen lain -- bukan
	// asal ganti tanpa lihat dokumen terkait.
	var contextParts []string
	switch target {
	case "prd":
		if s.IsComplete(store.FileDiscoveryQA) {
			v, _ := s.Load(store.FileDiscoveryQA)
			contextParts = append(contextParts, "Hasil Discovery:\n"+v)
		}
		if s.IsComplete(store.FileThreatReport) {
			v, _ := s.Load(store.FileThreatReport)
			contextParts = append(contextParts, "Threat Report:\n"+v)
		}
	case "schema":
		if s.IsComplete(store.FilePRD) {
			v, _ := s.Load(store.FilePRD)
			contextParts = append(contextParts, "PRD:\n"+v)
		}
	case "api":
		if s.IsComplete(store.FilePRD) {
			v, _ := s.Load(store.FilePRD)
			contextParts = append(contextParts, "PRD:\n"+v)
		}
		if s.IsComplete(store.FileSchema) {
			v, _ := s.Load(store.FileSchema)
			contextParts = append(contextParts, "Database Schema:\n"+v)
		}
	case "plan":
		if s.IsComplete(store.FilePRD) {
			v, _ := s.Load(store.FilePRD)
			contextParts = append(contextParts, "PRD:\n"+v)
		}
		if s.IsComplete(store.FileSchema) {
			v, _ := s.Load(store.FileSchema)
			contextParts = append(contextParts, "Database Schema:\n"+v)
		}
		if s.IsComplete(store.FileAPIContracts) {
			v, _ := s.Load(store.FileAPIContracts)
			contextParts = append(contextParts, "API Contracts:\n"+v)
		}
	}
	docContext := strings.Join(contextParts, "\n\n")

	fmt.Printf("== prdgen: revisi %s ==\n", t.file)
	fmt.Println("Tulis feedback kamu (akhiri dengan baris berisi EOF lalu Enter, atau Ctrl+D):")
	feedback := readMultiline(reader)
	if strings.TrimSpace(feedback) == "" {
		return fmt.Errorf("feedback tidak boleh kosong")
	}

	fmt.Println("\n[revise] mengirim feedback ke model...")
	revised, err := r.RunReviseDocument(ctx, currentDoc, feedback, docContext)
	if err != nil {
		return fmt.Errorf("stage revise: %w", err)
	}

	fmt.Println("\n--- Hasil Revisi ---")
	fmt.Println(revised)

	if !confirm(reader, fmt.Sprintf("\nTimpa %s dengan hasil revisi ini?", t.file)) {
		fmt.Println("Dibatalkan, file asli tidak berubah.")
		return nil
	}

	path, err := s.Save(t.file, revised)
	if err != nil {
		return err
	}
	fmt.Printf("✅ Tersimpan di %s\n", path)
	printDownstreamStalenessWarning(s, target)
	return nil
}

// printDownstreamStalenessWarning ingetin user kalau dokumen yang baru
// direvisi punya dokumen turunan yang sudah digenerate duluan dari versi
// LAMA -- dokumen turunan itu sekarang berpotensi tidak sinkron (state
// bug kalau didiamkan begitu saja tanpa pemberitahuan, karena
// determineLLDStartStage/runIssuesPipeline cuma cek "file-nya ada atau
// tidak", bukan "apakah masih konsisten dengan dokumen upstream yang baru
// direvisi"). Ini bukan auto-invalidate (sengaja, biar tidak ujug-ujug
// hilang tanpa persetujuan user) -- cuma warning supaya user sadar dan
// bisa putuskan sendiri mau revisi ulang dokumen turunannya atau tidak.
func printDownstreamStalenessWarning(s *store.Store, target string) {
	var downstream []string
	switch target {
	case "prd":
		downstream = []string{store.FileSchema, store.FileAPIContracts, store.FileCodingPlan, store.FileIssuesJSON}
	case "schema":
		downstream = []string{store.FileAPIContracts, store.FileCodingPlan, store.FileIssuesJSON}
	case "api":
		downstream = []string{store.FileCodingPlan, store.FileIssuesJSON}
	case "plan":
		downstream = []string{store.FileIssuesJSON}
	}

	var stale []string
	for _, f := range downstream {
		if s.IsComplete(f) {
			stale = append(stale, f)
		}
	}
	if len(stale) == 0 {
		return
	}
	fmt.Printf("\n⚠️  Perhatian: %s sudah digenerate dari versi %s yang LAMA (sebelum revisi ini),\n", strings.Join(stale, ", "), target)
	fmt.Println("   jadi sekarang berpotensi tidak sinkron. Kalau perubahan tadi cukup besar,")
	fmt.Println("   pertimbangkan jalankan 'prdgen revise' lagi untuk dokumen turunan itu juga,")
	fmt.Println("   atau hapus filenya lalu generate ulang lewat 'prdgen lld' / 'prdgen issues'.")
}

// readMultiline membaca input multi-baris dari terminal sampai user
// mengetik baris berisi "EOF" saja (case-insensitive) atau menekan
// Ctrl+D (EOF stream beneran).
//
// PENTING: dulu terminatornya baris kosong. Itu bug -- teks yang di-paste
// (misal jawaban buat beberapa pertanyaan sekaligus) hampir selalu punya
// baris kosong di tengah sebagai pemisah paragraf/pemisah antar-jawaban.
// Begitu ketemu baris kosong pertama, loop berhenti dan SISA baris yang
// belum sempat dibaca dari stdin tetap nyangkut di buffer -- lalu ke-baca
// diam-diam oleh prompt berikutnya (misal jadi auto-jawaban confirm()
// "Lanjut ke stage X?"), bikin state pipeline kelihatan "ngaco" padahal
// akar masalahnya cuma di sini. Sentinel "EOF" jauh lebih aman karena
// baris kosong di tengah paste tetap dianggap bagian dari isi, bukan
// penanda selesai.
func readMultiline(reader *bufio.Reader) string {
	var lines []string
	for {
		line, err := reader.ReadString('\n')
		hasContent := len(line) > 0
		trimmed := strings.TrimRight(line, "\r\n")
		if strings.EqualFold(strings.TrimSpace(trimmed), "EOF") {
			break
		}
		if hasContent {
			lines = append(lines, trimmed)
		}
		if err != nil {
			// Ctrl+D / EOF stream asli, bukan cuma baris kosong biasa.
			break
		}
	}
	return strings.Join(lines, "\n")
}

func confirm(reader *bufio.Reader, question string) bool {
	fmt.Printf("%s (y/n): ", question)
	line, _ := reader.ReadString('\n')
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes"
}
