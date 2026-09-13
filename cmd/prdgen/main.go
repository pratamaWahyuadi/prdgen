package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/pratamaWahyuadi/prdgen/internal/ghissues"
	"github.com/pratamaWahyuadi/prdgen/internal/llm"
	"github.com/pratamaWahyuadi/prdgen/internal/pipeline"
	"github.com/pratamaWahyuadi/prdgen/internal/store"
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

	s, err := store.New(projectDir)
	if err != nil {
		return err
	}

	// Knowledge injection (opt-in pasif): <project>/knowledge/*.md berisi
	// referensi domain user untuk teknologi kurang-umum (mis. Zitadel).
	// Di-load sekali di sini, di-inject ke SEMUA panggilan model via
	// Runner.complete -- discovery, security, PRD, LLD, issues, revisi,
	// validasi, semuanya dapat konteks yang sama. Tidak memengaruhi
	// resume (input read-only, bukan checkpoint).
	knowledge, err := s.LoadKnowledge()
	if err != nil {
		return err
	}
	if knowledge != "" {
		n := strings.Count(knowledge, "--- file: knowledge/")
		fmt.Printf("📚 Knowledge injection aktif: %d file referensi dari %s/knowledge/ (dipakai semua stage)\n", n, projectDir)
	}

	runner := &pipeline.Runner{Provider: provider, PromptDir: promptDir, Knowledge: knowledge}

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

	case "tokenharbor":
		// TokenHarbor (tokenharbor.ai) memakai protokol OpenAI-compatible
		// -- klien yang sama dengan DeepSeek, hanya beda endpoint.
		apiKey := os.Getenv("TOKENHARBOR_API_KEY")
		if apiKey == "" {
			return nil, fmt.Errorf("env TOKENHARBOR_API_KEY belum di-set")
		}
		model := os.Getenv("TOKENHARBOR_MODEL")
		if model == "" {
			model = "tokenharbor/qwen3-max"
		}
		return llm.NewTokenHarborProvider(apiKey, model), nil

	default:
		return nil, fmt.Errorf("LLM_PROVIDER tidak dikenal: %q (pilihan: deepseek, gemini, tokenharbor)", name)
	}
}

func printUsage() {
	fmt.Println(`Pemakaian:
  prdgen new <project-dir>                  discovery (2 fase + gate) -> security audit -> PRD -> validasi PRD
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
  LLM_PROVIDER        (opsional, default "deepseek") pilih "deepseek", "gemini", atau "tokenharbor"
  DEEPSEEK_API_KEY    (wajib kalau LLM_PROVIDER=deepseek) API key DeepSeek
  DEEPSEEK_MODEL      (opsional, default "deepseek-chat")
  GEMINI_API_KEY      (wajib kalau LLM_PROVIDER=gemini) API key Gemini (Google AI Studio)
  GEMINI_MODEL        (opsional, default "gemini-flash-latest")
  TOKENHARBOR_API_KEY (wajib kalau LLM_PROVIDER=tokenharbor) API key TokenHarbor (thk_live_...)
  TOKENHARBOR_MODEL   (opsional, default "tokenharbor/qwen3-max")
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

	var rawIdea, discoveryQA, productBrief, deepDiveQA, threatReport, prd string
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
	if s.IsComplete(store.FileProductBrief) {
		v, err := s.Load(store.FileProductBrief)
		if err != nil {
			return err
		}
		productBrief = v
	}
	if s.IsComplete(store.FileDeepDiveQA) {
		v, err := s.Load(store.FileDeepDiveQA)
		if err != nil {
			return err
		}
		deepDiveQA = v
	}
	if s.IsComplete(store.FileThreatReport) {
		v, err := s.Load(store.FileThreatReport)
		if err != nil {
			return err
		}
		threatReport = v
	}
	// BUGFIX (resume): tanpa blok ini, run yang resume langsung ke
	// StageValidatePRD membawa variabel prd = "" -- validator dikirim
	// draft PRD kosong dan jujur melaporkan "PRD kosong" padahal PRD.md
	// ada isinya. Kelas bug yang sama dengan resume deep-dive: state di
	// disk tidak pernah di-load kembali ke memori saat resume.
	if s.IsComplete(store.FilePRD) {
		v, err := s.Load(store.FilePRD)
		if err != nil {
			return err
		}
		prd = v
	}

	if stage == pipeline.StageDiscovery {
		if s.IsComplete(store.FileIdea) {
			// 00_idea.md sudah ada dari sesi sebelumnya (rawIdea sudah
			// di-load di atas). Jangan tanya lagi -- ini bug lama: sebelum
			// fix ini, kondisinya cuma cek stage == StageDiscovery tanpa
			// cek apakah ide-nya sendiri sudah tersimpan, jadi user selalu
			// diminta nulis ide ulang tiap kali resume ke stage discovery
			// (misal karena baru selesai jawab 01a_discovery_questions.md
			// secara manual tapi belum sempat generate 01b_discovery_qa.md).
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

			fmt.Println("\n--- Pertanyaan Discovery (Fase 1: High-Level) ---")
			fmt.Println(questions)
			fmt.Println("\nJawab semua pertanyaan di atas (akhiri dengan baris berisi EOF lalu Enter, atau Ctrl+D):")
			answers := readMultiline(reader)
			discoveryQA = questions + "\n\n=== Jawaban User ===\n" + answers
			if _, err := s.Save(store.FileDiscoveryQA, discoveryQA); err != nil {
				return err
			}

		case pipeline.StageDiscoveryBrief:
			fmt.Println("\n[brief] menyusun Product Brief dari jawaban fase 1...")
			brief, err := r.RunDiscoveryBrief(ctx, rawIdea, discoveryQA)
			if err != nil {
				return fmt.Errorf("stage discovery_brief: %w", err)
			}
			productBrief = brief
			path, err := s.Save(store.FileProductBrief, productBrief)
			if err != nil {
				return err
			}
			fmt.Println("\n--- Product Brief ---")
			fmt.Println(productBrief)
			fmt.Printf("\nTersimpan di %s\n", path)

		case pipeline.StageDiscoveryGate:
			// Gate eksplisit antara fase high-level dan deep-dive teknis.
			// User yang kewalahan boleh stop di sini; semua keputusan
			// low-level nanti diberi default eksplisit (defaults.yaml),
			// bukan ditebak diam-diam oleh agent.
			fmt.Println("\n--- Gate: Deep-Dive Teknis ---")
			fmt.Println("Product Brief di atas mengunci keputusan high-level (bahasa, framework,")
			fmt.Println("database engine, deployment). Yang BELUM dikunci: keputusan low-level yang")
			fmt.Println("berisiko ditebak ulang per-issue oleh coding agent nanti -- driver & pool,")
			fmt.Println("query layer, migration tool, cache/MQ/HTTP/storage client, config, error")
			fmt.Println("handling & logging, auth implementation, testing, CI/CD, struktur folder.")
			choice := askDeepDiveChoice(reader)
			switch choice {
			case "y":
				// Lanjut ke Technical Deep Dive (fase 2).
				stage = pipeline.StageDiscoveryDeep
				continue
			case "n":
				// Skip deep-dive: tulis defaults.yaml berisi default
				// eksplisit berbasis familiarity tim (dari Product Brief),
				// lalu langsung ke security. PRD/LLD wajib mengutip
				// file ini dan menandai semua nilainya [ASSUMED].
				fmt.Println("\n[gate] deep-dive di-skip. Menulis defaults.yaml dengan default eksplisit...")
				if err := writeDefaultsFromBrief(ctx, r, s, rawIdea, productBrief); err != nil {
					return err
				}
				stage = pipeline.StageSecurity
				continue
			default:
				return fmt.Errorf("dibatalkan oleh user di gate deep-dive")
			}

		case pipeline.StageDiscoveryDeep:
			var questions string
			if s.IsComplete(store.FileDeepDiveQuestions) {
				fmt.Println("\n[deep-dive] ditemukan pertanyaan dari sesi sebelumnya, lanjut dari situ.")
				v, err := s.Load(store.FileDeepDiveQuestions)
				if err != nil {
					return err
				}
				questions = v
			} else {
				fmt.Println("\n[deep-dive] menghubungi model (fase 2: keputusan low-level)...")
				v, err := r.RunDiscoveryDeep(ctx, rawIdea, productBrief)
				if err != nil {
					return fmt.Errorf("stage discovery_deep: %w", err)
				}
				questions = v
				if _, err := s.Save(store.FileDeepDiveQuestions, questions); err != nil {
					return err
				}
			}

			fmt.Println("\n--- Pertanyaan Technical Deep Dive (Fase 2: Low-Level) ---")
			fmt.Println(questions)
			fmt.Println("\nJawab semua pertanyaan di atas (akhiri dengan baris berisi EOF lalu Enter, atau Ctrl+D):")
			answers := readMultiline(reader)
			deepDiveQA = productBrief + "\n\n=== Pertanyaan Deep Dive ===\n" + questions + "\n\n=== Jawaban User ===\n" + answers
			if _, err := s.Save(store.FileDeepDiveQA, deepDiveQA); err != nil {
				return err
			}
			// Deep-dive selesai: hapus defaults.yaml generik kalau ada dari
			// run sebelumnya yang skip -- keputusan low-level sekarang
			// dijawab user langsung, file default jadi menyesatkan.
			if s.Exists(store.FileDefaultsYAML) {
				if err := os.Remove(filepath.Join(s.Dir, store.FileDefaultsYAML)); err != nil {
					fmt.Printf("⚠️  Gagal menghapus defaults.yaml usang (abaikan kalau memang mau disimpan): %v\n", err)
				} else {
					fmt.Println("[deep-dive] defaults.yaml (dari mode skip) dihapus -- keputusan low-level sudah dijawab eksplisit.")
				}
			}

		case pipeline.StageSecurity:
			fmt.Println("\n[security] menjalankan threat modeling...")
			// Konteks keamanan: seluruh hasil discovery (fase 1 + deep-dive
			// kalau ada) supaya threat spesifik ke driver/auth/deployment
			// yang benar-benar dipilih, bukan generik.
			securityCtx := discoveryQA
			if deepDiveQA != "" {
				securityCtx = discoveryQA + "\n\n=== Hasil Deep Dive (low-level) ===\n" + deepDiveQA
			}
			report, err := r.RunSecurity(ctx, rawIdea, securityCtx)
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
			printSanityWarnings(threatReport, pipeline.DocOther)
			if !confirm(reader, "Lanjut ke generate PRD?") {
				return fmt.Errorf("dibatalkan oleh user di stage security")
			}

		case pipeline.StagePRD:
			fmt.Println("\n[prd] menggenerate PRD final...")
			// Keputusan low-level: dari deep-dive (kalau user ikut) atau
			// defaults.yaml (kalau skip) -- dua-duanya wajib dikutip PRD
			// di section Asumsi Teknis, jangan ditebak ulang.
			deepDiveCtx := deepDiveQA
			if deepDiveCtx == "" && s.IsComplete(store.FileDefaultsYAML) {
				v, err := s.Load(store.FileDefaultsYAML)
				if err != nil {
					return err
				}
				deepDiveCtx = "ISI defaults.yaml (user skip deep-dive; SEMUA nilai di bawah berstatus assumed/[ASSUMED]):\n" + v
			}
			doc, err := r.RunPRD(ctx, rawIdea, discoveryQA, threatReport, deepDiveCtx)
			if err != nil {
				return fmt.Errorf("stage prd: %w", err)
			}
			prd = doc
			printSanityWarnings(prd, pipeline.DocPRD)
			path, err := s.Save(store.FilePRD, prd)
			if err != nil {
				return err
			}
			fmt.Printf("\n✅ PRD selesai, tersimpan di %s\n", path)
			fmt.Println("Lanjutkan dengan: prdgen lld <project-dir>")

		case pipeline.StageValidatePRD:
			fmt.Println("\n[validate] mengecek konsistensi PRD vs discovery...")
			// Validator melihat SELURUH konteks discovery: fase 1 + deep-dive
			// (kalau ada) + defaults.yaml (kalau skip) -- supaya coverage
			// jawaban dan ketepatan flag [ASSUMED] bisa dinilai.
			validateCtx := buildValidateCtx(s, discoveryQA, deepDiveQA)
			report, err := r.RunValidatePRD(ctx, validateCtx, prd)
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

			// Loop revisi pasca-validasi: temuan yang baru dibaca nggak
			// seharusnya berakhir di "baca file-nya sendiri di editor".
			// User bisa: (a) revisi otomatis dari laporan, (b) tulis
			// feedback sendiri, (c) selesai. Tiap revisi otomatis
			// diikuti re-validasi supaya PRD_VALIDATION.md tidak basi.
			// Batas round mencegah loop revisi<->validasi tak berujung
			// (tiap round = 2 panggilan LLM).
			for round := 1; round <= maxAutoReviseRounds; round++ {
				switch askPostValidationChoice(reader, round, maxAutoReviseRounds) {
				case "r":
					fmt.Println("\n[revise] merevisi PRD berdasarkan laporan validasi...")
					revised, err := r.RunReviseDocument(ctx, prd, "Perbaiki PRD ini berdasarkan laporan validasi di bawah. Tangani SEMUA temuan (item terlewat + kontradiksi) satu per satu: item terlewat -> tambahkan ke section yang disebut laporan; kontradiksi -> jawaban discovery user SELALU menang atas isi PRD sekarang (kutip bagian yang salah lalu ganti sesuai jawaban user). Pertahankan ID threat T1/T2/... dan semua flag [ASSUMED]/user-confirmed apa adanya. Jangan ubah bagian yang tidak ditemukan bermasalah.\n\nLAPORAN VALIDASI:\n"+report, buildReviseCtx(s, discoveryQA, deepDiveQA, threatReport))
					if err != nil {
						fmt.Printf("⚠️  Revisi gagal (%v) -- PRD tidak berubah.\n", err)
						continue
					}
					fmt.Println("\n--- PRD hasil revisi ---")
					fmt.Println(revised)
					if !confirm(reader, "\nTimpa PRD.md dengan hasil revisi ini?") {
						fmt.Println("Dibatalkan, PRD asli tidak berubah.")
						continue
					}
					prd = revised
					printSanityWarnings(prd, pipeline.DocPRD)
					if _, err := s.Save(store.FilePRD, prd); err != nil {
						return err
					}
					fmt.Println("✅ PRD direvisi. Menjalankan ulang validasi...")
					validateCtx := buildValidateCtx(s, discoveryQA, deepDiveQA)
					newReport, err := r.RunValidatePRD(ctx, validateCtx, prd)
					if err != nil {
						fmt.Printf("⚠️  Re-validasi gagal (%v) -- PRD tersimpan, tapi laporan validasi lama tetap dipakai.\n", err)
						continue
					}
					report = newReport
					if _, err := s.Save(store.FilePRDValidation, report); err != nil {
						return err
					}
					fmt.Println("\n--- Hasil Validasi Ulang ---")
					fmt.Println(report)
				case "m":
					fmt.Println("Tulis feedback kamu (akhiri dengan baris berisi EOF lalu Enter, atau Ctrl+D):")
					feedback := readMultiline(reader)
					if strings.TrimSpace(feedback) == "" {
						fmt.Println("Feedback kosong, dibatalkan.")
						continue
					}
					revised, err := r.RunReviseDocument(ctx, prd, feedback, buildReviseCtx(s, discoveryQA, deepDiveQA, threatReport))
					if err != nil {
						fmt.Printf("⚠️  Revisi gagal (%v) -- PRD tidak berubah.\n", err)
						continue
					}
					fmt.Println("\n--- PRD hasil revisi ---")
					fmt.Println(revised)
					if !confirm(reader, "\nTimpa PRD.md dengan hasil revisi ini?") {
						fmt.Println("Dibatalkan, PRD asli tidak berubah.")
						continue
					}
					prd = revised
					printSanityWarnings(prd, pipeline.DocPRD)
					if _, err := s.Save(store.FilePRD, prd); err != nil {
						return err
					}
					fmt.Println("✅ PRD direvisi. Menjalankan ulang validasi...")
					validateCtx := buildValidateCtx(s, discoveryQA, deepDiveQA)
					newReport, err := r.RunValidatePRD(ctx, validateCtx, prd)
					if err != nil {
						fmt.Printf("⚠️  Re-validasi gagal (%v) -- PRD tersimpan, tapi laporan validasi lama tetap dipakai.\n", err)
						continue
					}
					report = newReport
					if _, err := s.Save(store.FilePRDValidation, report); err != nil {
						return err
					}
					fmt.Println("\n--- Hasil Validasi Ulang ---")
					fmt.Println(report)
				default:
					// "s"/selesai/kosong -- user puas atau mau review manual
					// dulu di editor. Round tidak dipakai.
					return nil
				}
			}
		}
		stage = stage.Next()
	}
	return nil
}

// maxAutoReviseRounds membatasi loop revisi-otomatis<->re-validasi. Setiap
// round = 2 panggilan LLM (revisi + validasi ulang); tanpa batas, LLM yang
// selalu menemukan "sesuatu" bikin loop tak berujung dan tagihan API.
const maxAutoReviseRounds = 3

// buildValidateCtx merangkai konteks discovery lengkap untuk validator:
// fase 1 + deep-dive (kalau ada) + defaults.yaml (kalau user skip). Satu
// tempat supaya panggilan pertama dan re-validasi setelah revisi konsisten.
func buildValidateCtx(s *store.Store, discoveryQA, deepDiveQA string) string {
	validateCtx := discoveryQA
	if deepDiveQA != "" {
		validateCtx += "\n\n=== Jawaban Deep Dive ===\n" + deepDiveQA
	} else if s.IsComplete(store.FileDefaultsYAML) {
		v, _ := s.Load(store.FileDefaultsYAML)
		validateCtx += "\n\n=== defaults.yaml (user skip deep-dive) ===\n" + v
	}
	return validateCtx
}

// buildReviseCtx merangkai dokumen konteks pendukung untuk revisi PRD
// (sama dengan konteks command `prdgen revise prd`): hasil discovery +
// deep-dive + threat report, supaya revisi pasca-validasi tidak
// mengarang tanpa dasar jawaban user.
func buildReviseCtx(s *store.Store, discoveryQA, deepDiveQA, threatReport string) string {
	parts := []string{"Hasil Discovery:\n" + discoveryQA}
	if deepDiveQA != "" {
		parts = append(parts, "Hasil Deep Dive:\n"+deepDiveQA)
	}
	if threatReport != "" {
		parts = append(parts, "Threat Report:\n"+threatReport)
	}
	return strings.Join(parts, "\n\n")
}

// askPostValidationChoice menanyarkan langkah lanjut setelah laporan
// validasi tampil. Return: "r" = revisi otomatis dari laporan, "m" =
// feedback manual, apapun selain itu = selesai.
func askPostValidationChoice(reader *bufio.Reader, round, maxRounds int) string {
	fmt.Printf("\nSekarang mau gimana? (r=revise PRD otomatis dari laporan di atas, m=revise dengan feedback kamu sendiri, Enter=selesai) [round %d/%d]: ", round, maxRounds)
	line, _ := reader.ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "r", "revise":
		return "r"
	case "m", "manual":
		return "m"
	default:
		return "s"
	}
}

// determineStartStage menentukan stage mulai untuk `prdgen new` berdasarkan
// file mana yang sudah ada. Urutan cek MENGHORMATI gate deep-dive: kalau
// deep-dive di-skip (tidak ada 01e_deep_dive_qa.md) tapi defaults.yaml sudah
// ditulis, itu tanda user melewati gate dengan jalan "skip", jadi resume
// langsung ke security -- bukan memaksa user menjawab deep-dive lagi.
// Kehadiran 01d_deep_dive_questions.md juga dihormati sebagai tanda user
// sudah pernah memilih "y" di gate: kalau dia berhenti di tengah menjawab,
// resume langsung ke deep-dive (pertanyaan tersimpan di-load ulang), bukan
// diminta menjawab gate lagi.
func determineStartStage(s *store.Store) pipeline.Stage {
	if !s.IsComplete(store.FileDiscoveryQA) {
		return pipeline.StageDiscovery
	}
	if !s.IsComplete(store.FileProductBrief) {
		return pipeline.StageDiscoveryBrief
	}
	if s.IsComplete(store.FileDeepDiveQA) || s.IsComplete(store.FileDefaultsYAML) {
		// Gate sudah dilewati (jalur manapun) -- lanjut ke stage berikutnya.
	} else if s.IsComplete(store.FileDeepDiveQuestions) {
		// 01d ada = user sudah pilih "y" di gate dan pertanyaan fase 2
		// tersimpan; dia berhenti di tengah menjawab. Resume langsung ke
		// deep-dive, JANGAN minta keputusan gate ulang.
		return pipeline.StageDiscoveryDeep
	} else {
		return pipeline.StageDiscoveryGate
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

// askDeepDiveChoice menanyakan gate eksplisit antara fase 1 (high-level) dan
// fase 2 (deep-dive teknis). Input tak dikenal/salah ketik di-re-prompt
// (maksimal 3x) -- JANGAN langsung abort: membatalkan seluruh run hanya
// karena user salah ketik itu UX yang jauh lebih buruk daripada nanya ulang.
// Return "y"/"n"/"q" (q = berhenti, hanya kalau user eksplisit nulis q).
func askDeepDiveChoice(reader *bufio.Reader) string {
	const maxAttempts = 3
	fmt.Println("\nMau lanjut ke deep-dive teknis (driver, connection pool, query layer,")
	fmt.Print("migration tool, cache/MQ/storage client, config, error handling & logging, auth, testing, CI/CD, struktur folder)? (y=lanjut, n=skip & pakai default eksplisit, q=berhenti): ")
	for attempt := 1; ; attempt++ {
		line, _ := reader.ReadString('\n')
		switch strings.ToLower(strings.TrimSpace(line)) {
		case "y", "yes":
			return "y"
		case "n", "no":
			return "n"
		case "q", "quit":
			return "q"
		}
		if attempt >= maxAttempts {
			fmt.Println("Input tidak dikenal 3x -- berhenti demi aman (tidak ada pilihan yang diambil tanpa persetujuan).")
			return "q"
		}
		fmt.Printf("Pilihan tidak dikenal (%d/%d). Ketik y=lanjut, n=skip & pakai default eksplisit, q=berhenti: ", attempt, maxAttempts)
	}
}

// writeDefaultsFromBrief dipanggil saat user SKIP deep-dive di gate: generate
// defaults.yaml berisi default eksplisit untuk semua keputusan low-level
// (berbasis familiarity tim di Product Brief), lalu tampilkan ke user
// sebelum disimpan supaya tetap ada momen review.
func writeDefaultsFromBrief(ctx context.Context, r *pipeline.Runner, s *store.Store, rawIdea, productBrief string) error {
	out, err := r.RunGenerateDefaults(ctx, rawIdea, productBrief)
	if err != nil {
		return err
	}
	fmt.Println("\n--- defaults.yaml (default eksplisit, semua flagged [ASSUMED]) ---")
	fmt.Println(out)
	path, err := s.Save(store.FileDefaultsYAML, out)
	if err != nil {
		return err
	}
	fmt.Printf("\nTersimpan di %s\n", path)
	fmt.Println("PRD dan LLD berikutnya WAJIB mengutip file ini di section Asumsi Teknis")
	fmt.Println("dan menandai semua nilainya [ASSUMED] -- coding agent dilarang membuat")
	fmt.Println("instance/pool/driver kedua untuk komponen yang sama, dan wajib stop &")
	fmt.Println("tanya kalau default ternyata tidak cocok saat coding.")
	return nil
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
			printSanityWarnings(schema, pipeline.DocSchema)
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
			printSanityWarnings(apiContracts, pipeline.DocAPI)
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
			printSanityWarnings(codingPlan, pipeline.DocPlan)
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

	// Sanity check mekanis (tanpa LLM, gratis) sebelum issue dibuat ke
	// GitHub -- penting terutama untuk mode --yes di mana review manual
	// per-issue di-skip total. Temuan hanya WARNING; user tetap lanjut
	// kalau mau (revisi issue via 'e' tetap tersedia di mode interaktif).
	if findings := ghissues.ValidateDraft(issues, codingPlan); len(findings) > 0 {
		fmt.Printf("\n⚠️  Sanity check draft (mekanis, tanpa LLM) menemukan %d temuan:\n", len(findings))
		for _, f := range findings {
			fmt.Printf("   - %s\n", f.Error())
		}
		fmt.Println("   (temuan ini bukan blocker; lewati dengan sadar, atau perbaiki lewat 'e' saat review per-issue)")
	}

	alreadyCreated, err := loadCreatedIssueTitles(s)
	if err != nil {
		return err
	}

	// Deteksi mismatch judul draft vs log: judul di ISSUES_CREATED.log yang
	// TIDAK cocok dengan manapun judul di draft berarti draft sudah
	// di-regenerate/di-revise setelah issue itu dibuat -- run ini tidak akan
	// membuat issue penggantinya (skip by-title), jadi beri tahu user secara
	// eksplisit daripada diam-diam menganggap semua aman.
	if orphaned := findLoggedTitlesMissingFromDraft(alreadyCreated, issues); len(orphaned) > 0 {
		fmt.Printf("\n⚠️  Ada %d judul di ISSUES_CREATED.log yang tidak ada di draft sekarang:\n", len(orphaned))
		for _, t := range orphaned {
			fmt.Printf("   - %s\n", t)
		}
		fmt.Println("   Kemungkinan draft sudah di-regenerate/revisi setelah issue di atas dibuat.")
		fmt.Println("   Issue lama tetap ada di GitHub; run ini TIDAK akan membuat issue penggantinya")
		fmt.Println("   (skip berdasarkan judul hanya berlaku untuk judul yang persis sama). Kalau kamu")
		fmt.Println("   sengaja mengganti issue lama dengan yang baru, review manual dulu sebelum lanjut.")
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
		title, _, _ := strings.Cut(line, "	")
		titles[title] = true
	}
	return titles, nil
}

// findLoggedTitlesMissingFromDraft membandingkan judul di ISSUES_CREATED.log
// dengan judul di draft sekarang. Judul yang tercatat di log tapi tidak ada
// di draft berarti draft diganti setelah issue dibuat (revise/regenerate) --
// kalau tidak diumumkan, issue pengganti judul-baru itu bakal dibuat dobel
// (log tidak match) atau issue lama diam-diam dianggap masih terwakili.
func findLoggedTitlesMissingFromDraft(logged map[string]bool, issues []ghissues.Issue) []string {
	draftTitles := make(map[string]bool, len(issues))
	for _, iss := range issues {
		draftTitles[iss.Title] = true
	}
	var missing []string
	for t := range logged {
		if !draftTitles[t] {
			missing = append(missing, t)
		}
	}
	sort.Strings(missing)
	return missing
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

// printSanityWarnings menjalankan pemeriksaan struktural murah terhadap
// dokumen yang baru di-generate dan mencetak temuan sebagai WARNING sebelum
// dokumen dikonfirmasi/disimpan. Tidak pernah memblok -- user tetap pegang
// keputusan (hormat pada konfirmasi y/n yang sudah ada), tapi temuan
// truncation/format yang jelas cacat tidak lagi lolos diam-diam.
func printSanityWarnings(doc string, kind pipeline.DocKind) {
	findings := pipeline.SanityCheck(doc, kind)
	if len(findings) == 0 {
		return
	}
	fmt.Printf("\n⚠️  Sanity check menemukan %d temuan pada dokumen ini:\n", len(findings))
	for _, f := range findings {
		fmt.Printf("   - %s\n", f)
	}
	fmt.Println("   (periksa dokumen sebelum lanjut -- terutama kalau temuan menyebut terpotong)")
}
