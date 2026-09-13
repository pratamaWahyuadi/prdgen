package ghissues

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// ValidationError merepresentasikan satu temuan dari ValidateDraft. Satu
// baris per temuan supaya bisa langsung ditampilkan ke user sebagai daftar
// yang bisa di-scan -- bukan satu error gabungan yang susah dibaca.
type ValidationError struct {
	Issue int    // index issue (1-based); 0 kalau temuan level-draft (bukan per-issue)
	Rule  string // nama rule yang dilanggar, mis. "phase-matches-plan"
	Msg   string // penjelasan spesifik + cara memperbaiki
}

func (e ValidationError) Error() string {
	if e.Issue > 0 {
		return fmt.Sprintf("issue #%d: [%s] %s", e.Issue, e.Rule, e.Msg)
	}
	return fmt.Sprintf("[draft] [%s] %s", e.Rule, e.Msg)
}

// ValidateDraft menjalankan sanity check MEKANIS (tanpa LLM, gratis, deterministik)
// terhadap draft issues sebelum dibuat ke GitHub. Ini pertahanan untuk dua
// skenario nyata:
//
//  1. Agent issues yang diam-diam menyimpang dari coding plan: memakai nama
//     fase yang tidak ada di plan, atau lupa menyebut file sumber koneksi
//     database padahal issue-nya jelas menyentuh DB (kelas bug double-pool
//     berulang kalau tidak ditangkap di sini).
//  2. Mode --yes: review manual per-issue di-skip total, jadi pemeriksaan
//     mekanis ini satu-satunya gerbang sebelum `gh issue create`.
//
// Semua temuan yang dikembalikan adalah WARNING, bukan blocker: draft tetap
// boleh dipakai (fungsi ini tidak pernah gagal), tapi setiap temuan
// ditampilkan ke user supaya bisa memutuskan sendiri -- revise ('e'),
// skip, atau lanjut dengan sadar.
func ValidateDraft(issues []Issue, codingPlan string) []ValidationError {
	var errs []ValidationError

	knownPhases := extractPlanPhases(codingPlan)
	connFiles := extractConnectionSourceFiles(codingPlan)

	// Normalisasi nama fase sekali sebelum loop: heading plan dan field
	// phase issue dibandingkan dalam bentuk ternormalisasi (lowercase,
	// whitespace collapse) supaya perbedaan kecil spasi/tanda baca dari
	// LLM tidak memicu warning palsu. Pesan error tetap menampilkan
	// teks asli biar user mudah mengenali.
	normPhases := map[string]bool{}
	for p := range knownPhases {
		normPhases[normalizePhase(p)] = true
	}

	for i, iss := range issues {
		num := i + 1

		// Rule 1: "phase" issue HARUS sama dengan salah satu nama fase yang
		// ada di coding plan. Dibandingkan TERNORMALISASI (case-insensitive,
		// whitespace collapse) -- verbatim strict terlalu rapuh terhadap
		// variasi kecil output LLM, tapi makna "fase yang sama" tetap
		// terjaga. Ini nyegah agent issues mengarang urutan/label fase
		// sendiri -- kalau fase tidak dikenal, urutan pengerjaan
		// (phase-1, phase-2, ...) jadi ngaco dan agent coding bingung
		// issue mana dulu.
		if len(normPhases) > 0 && !normPhases[normalizePhase(iss.Phase)] {
			suggestion := fmt.Sprintf(" Fase yang dikenal dari plan: %s.", strings.Join(sortedKeys(knownPhases), ", "))
			errs = append(errs, ValidationError{
				Issue: num,
				Rule:  "phase-matches-plan",
				Msg: fmt.Sprintf("field phase=%q tidak cocok dengan nama fase manapun di Coding Plan (dibandingkan case-insensitive, whitespace di-collapse).%s",
					iss.Phase, suggestion),
			})
		}

		// Rule 2: issue yang menyentuh database/cache/queue WAJIB menyebut
		// file sumber koneksi/pool (ambil dari plan) supaya agent coding
		// reuse, bukan bikin pool baru. Deteksi kasar: body menyebut
		// kata kunci DB-ish TIDAK mengandung path file koneksi yang sudah
		// dikunci di plan. Cek ini sengaja HIGH-RECALL (kata kunci luas) --
		// lebih baik false positive yang bisa diabaikan user daripada
		// double-pool lolos diam-diam.
		if len(connFiles) > 0 && touchesSharedInfra(iss.Body) && !mentionsAnyFile(iss.Body, connFiles) {
			errs = append(errs, ValidationError{
				Issue: num,
				Rule:  "db-conn-source-cited",
				Msg: fmt.Sprintf("issue ini menyentuh database/cache/queue tapi body-nya tidak menyebut file sumber koneksi/pool yang dikunci di Coding Plan (%s). "+
					"Agent coding yang mengerjakan issue ini berisiko membuat koneksi/pool baru sendiri. Tambahkan rujukan eksplisit ke file tersebut (mis. di Technical Notes).",
					strings.Join(sortedFiles(connFiles), ", ")),
			})
		}
	}
	return errs
}

// touchesSharedInfra mendeteksi apakah body issue jelas-jelas menyentuh
// komponen shared-instance (DB/cache/queue/HTTP client eksternal).
//
// TWO-TIER (bukan satu threshold flat):
//   - Kata KUAT (postgres, migration, sqlc, dsn, dst.) sudah spesifik
//     dengan sendirinya menandakan infra -- SATU match cukup memicu rule.
//     Contoh nyata yang harus tetap tertangkap: "tambah migration untuk
//     tabel users" (satu keyword kuat: migration).
//   - Kata AMBIGU (pool, connection, repository) sering muncul di konteks
//     non-infra ("connection pool" di config HTTP client, "repository
//     pattern" di diskusi arsitektur) -- butuh minimal DUA match BERBEDA
//     sebelum memicu, supaya satu kata generik tidak bikin noise warning
//     yang lama-lama diabaikan user (alarm fatigue).
func touchesSharedInfra(body string) bool {
	lower := strings.ToLower(body)
	for _, kw := range strongInfraKeywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	matches := 0
	for _, kw := range ambiguousInfraKeywords {
		if strings.Contains(lower, kw) {
			matches++
		}
	}
	return matches >= 2
}

// strongInfraKeywords: satu match sudah cukup -- kata-kata ini hampir
// pasti hanya muncul saat issue benar-benar menyentuh infra shared-instance.
var strongInfraKeywords = []string{
	"postgres", "mysql", "sqlite", "redis", "kafka", "rabbitmq",
	"sqlc", "gorm", "prisma", "dsn", "migration", "database",
	"s3", "minio", "bucket", "object storage", "file storage",
}

// ambiguousInfraKeywords: butuh pasangan (2 match berbeda) -- maknanya
// bergantung konteks, satu kata sendirian tidak cukup meyakinkan.
var ambiguousInfraKeywords = []string{"pool", "connection", "repository"}

// planPhasePattern: mencocokkan baris judul fase di coding plan, contoh:
//
//	"## Fase 1: Setup & DB Migrations"
//	"### Fase 2 — Core Domain Logic"
var planPhasePattern = regexp.MustCompile(`(?im)^#{1,6}\s*(fase|phase|tahap)\s*\d+.*$`)

// extractPlanPhases mengembalikan set nama fase (verbatim, termasuk
// penomoran) yang muncul sebagai heading di coding plan. Issue wajib memakai
// nama fase persis dari sini.
func extractPlanPhases(codingPlan string) map[string]bool {
	phases := map[string]bool{}
	for _, m := range planPhasePattern.FindAllString(codingPlan, -1) {
		// Buang prefix "##" dan whitespace berlebih.
		name := strings.TrimLeft(m, "# \t")
		name = strings.TrimSpace(name)
		if name != "" {
			phases[name] = true
		}
	}
	return phases
}

// connFilePattern: mencocokkan path file yang disebut plan sebagai tempat
// koneksi/pool diinisialisasi, contoh dari instruksi "Setup Koneksi Database
// (Single Source of Truth)" yang menyebut `internal/db/db.go` -- pola:
// backtick + path dengan ekstensi .go (atau file umum lain).
var connFilePattern = regexp.MustCompile("`([^`]+\\.(?:go|py|ts|js|java|rb|php))`")

// extractConnectionSourceFiles memanen path file sumber koneksi dari coding
// plan. Cara paling andal yang tersedia tanpa parsing semantik: kumpulkan
// semua path file ber-backtick yang muncul dalam satu baris bersama kata
// kunci koneksi/pool, plus semua path ber-backtick yang muncul di baris
// ber-kata "koneksi", "pool", "client", "connection" (baris Fase-1 setup).
func extractConnectionSourceFiles(codingPlan string) map[string]bool {
	files := map[string]bool{}
	keywords := []string{"koneksi", "connection", "pool", "client", "db.go"}
	for _, line := range strings.Split(codingPlan, "\n") {
		lower := strings.ToLower(line)
		isConnLine := false
		for _, kw := range keywords {
			if strings.Contains(lower, kw) {
				isConnLine = true
				break
			}
		}
		if !isConnLine {
			continue
		}
		for _, m := range connFilePattern.FindAllStringSubmatch(line, -1) {
			if len(m) >= 2 {
				path := strings.TrimSpace(m[1])
				if path != "" {
					files[path] = true
				}
			}
		}
	}
	return files
}

// mentionsAnyFile cek apakah body issue menyebut salah satu file koneksi
// (dari coding plan) secara eksplisit.
func mentionsAnyFile(body string, files map[string]bool) bool {
	for f := range files {
		if strings.Contains(body, f) {
			return true
		}
	}
	return false
}

// normalizePhase menyeragamkan nama fase sebelum dibandingkan: lowercase,
// collapse semua whitespace beruntun jadi satu spasi, trim. Tanda baca
// (":" vs "—" vs "-") sengaja TIDAK dinormalisasi penuh -- dua fase yang
// beda hanya tanda baca masih layap dicek manual, karena bisa jadi dua
// fase berbeda yang kebetulan mirip; spasi/case yang beda hampir pasti
// cuma variasi formatting dari LLM.
func normalizePhase(s string) string {
	lower := strings.ToLower(strings.TrimSpace(s))
	return strings.Join(strings.Fields(lower), " ")
}

// sortedKeys mengembalikan keys map sebagai slice terurut (untuk pesan error
// yang deterministik dan mudah dibaca).
func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// sortedFiles alias sortedKeys (nama deskriptif untuk konteks file).
func sortedFiles(m map[string]bool) []string { return sortedKeys(m) }
