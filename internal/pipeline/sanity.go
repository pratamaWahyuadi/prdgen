package pipeline

import (
	"regexp"
	"strings"
)

// DocKind jenis dokumen yang sedang dicek, menentukan pemeriksaan spesifik
// apa yang dijalankan SanityCheck.
type DocKind int

const (
	// DocPRD, DocSchema, DocAPI, DocPlan: dokumen inti pipeline.
	DocPRD DocKind = iota
	DocSchema
	DocAPI
	DocPlan
	// DocOther dokumen pendukung (threat report, validasi, revisi) tanpa
	// struktur khusus yang wajib ada.
	DocOther
)

// SanityCheck menjalankan pemeriksaan struktural murah (tanpa LLM, tanpa biaya)
// terhadap dokumen yang baru di-generate. Tujuannya menangkap output yang
// jelas cacat secara format -- bukan penilaian kualitas isi (itu tugas
// validator LLM), tapi jejak bahwa dokumen kemungkinan besar terpotong,
// gagal render, atau kosong substansinya.
//
// Hasilnya adalah list temuan WARNING (bukan error fatal): caller
// menampilkan temuan ke user sebelum menyimpan/melanjutkan, dan user tetap
// memegang keputusan akhir -- konsisten dengan filosofi tool ini yang tidak
// pernah diam-diam menimpa keputusan user.
func SanityCheck(doc string, kind DocKind) []string {
	var findings []string

	trimmed := strings.TrimSpace(doc)
	if trimmed == "" {
		return []string{"dokumen kosong total"}
	}

	if f := checkUnclosedCodeFence(trimmed); f != "" {
		findings = append(findings, f)
	}
	if f := checkLastLineCut(trimmed); f != "" {
		findings = append(findings, f)
	}

	switch kind {
	case DocPRD:
		findings = append(findings, checkPRDRequiredSections(trimmed)...)
	case DocSchema:
		findings = append(findings, checkMermaid(trimmed)...)
		findings = append(findings, checkTableDetail(trimmed)...)
	case DocAPI:
		findings = append(findings, checkEndpoints(trimmed)...)
	case DocPlan:
		findings = append(findings, checkPlanPhases(trimmed)...)
	}
	return findings
}

// prdSectionPattern mencocokkan heading/penyebutan section wajib PRD secara
// longgar: heading markdown "## 7.5 Asumsi Teknis" maupun penyebutan inline
// "section 7.5 Asumsi Teknis". Case-insensitive, spasi fleksibel.
var (
	prdAssumptionPattern = regexp.MustCompile(`(?i)(?:^#{1,6}.*|#?\s*section\s*)?\d?\.?\s*7\.?5?\s*[-–—:]?\s*asumsi\s+teknis`)
	prdSharedInstPattern = regexp.MustCompile(`(?i)instance\s+terbagi\s+lain`)
	prdDriverPattern     = regexp.MustCompile(`(?i)koneksi\s*&?\s*driver\s+database`)
)

// checkPRDRequiredSections: prompt prd.txt kini mewajibkan tiga section
// kontrak yang akan dikutip turun-temurun oleh ERD/plan/issues (driver &
// pool, instance terbagi lain, asumsi teknis [ASSUMED]). Kalau LLM lupa
// menulisnya, tidak ada downstream yang menangkapnya secara mekanis --
// persis kelas gap yang bikin sanity.go dibuat. Cek di sini supaya
// warning muncul SEBELUM dokumen dikonfirmasi user.
func checkPRDRequiredSections(doc string) []string {
	var findings []string
	if !prdDriverPattern.MatchString(doc) {
		findings = append(findings, "tidak ditemukan sub-section 'Koneksi & Driver Database' -- wajib ada di PRD karena dikutip verbatim oleh ERD/plan/issues (anti double-pool)")
	}
	if !prdSharedInstPattern.MatchString(doc) {
		findings = append(findings, "tidak ditemukan sub-section 'Instance Terbagi Lain' (cache/MQ/HTTP client/migration tool) -- wajib ada supaya agent LLD tidak menebak instance sendiri")
	}
	if !prdAssumptionPattern.MatchString(doc) {
		findings = append(findings, "tidak ditemukan section '7.5 Asumsi Teknis' -- wajib ada berisi tabel keputusan low-level + flag [ASSUMED] (kontrak untuk coding agent)")
	}
	return findings
}

// checkUnclosedCodeFence: jumlah ``` ganjil berarti ada code fence yang
// dibuka tapi tidak pernah ditutup -- salah satu jejak truncation paling
// umum untuk dokumen teknis berisi blok SQL/JSON/mermaid.
func checkUnclosedCodeFence(doc string) string {
	n := strings.Count(doc, "```")
	if n%2 != 0 {
		return "ada code fence (```) yang dibuka tapi tidak ditutup -- kemungkinan besar dokumen terpotong di tengah"
	}
	return ""
}

// checkLastLineCut: baris terakhir yang terlihat seperti prosa (ada spasi,
// cukup panjang) tapi tidak diakhiri penutup apapun (titik, tanda baca,
// penutup bracket/kutip/fence) -- suspect terpotong.
func checkLastLineCut(doc string) string {
	lines := strings.Split(doc, "\n")
	var last string
	for i := len(lines) - 1; i >= 0; i-- {
		l := strings.TrimSpace(lines[i])
		if l != "" {
			last = l
			break
		}
	}
	if len(last) < 40 || !strings.Contains(last, " ") {
		return ""
	}
	lastRune := last[len(last)-1]
	switch lastRune {
	case '.', '!', '?', ':', ';', ')', ']', '}', '"', '\'', '`', '|', '*', '-', '_':
		return ""
	}
	return "baris terakhir dokumen terlihat seperti kalimat yang terpotong di tengah (tidak berakhir dengan penutup yang wajar) -- kemungkinan output kena limit token"
}

// checkMermaid: schema WAJIB punya ERD dalam format Mermaid.js (sesuai prompt
// lld_erd). Fence ```mermaid yang dibuka tanpa ditutup juga dicek ulang di
// sini secara spesifik (checkUnclosedCodeFence cuma bilang "fence" generik).
func checkMermaid(doc string) []string {
	var findings []string
	n := strings.Count(doc, "```mermaid")
	switch {
	case n == 0:
		findings = append(findings, "tidak ditemukan blok ```mermaid -- ERD dalam format Mermaid.js adalah output wajib schema")
	case n > 1 && strings.Count(doc, "```") < 2*n:
		// total fence harus >= 2x jumlah fence mermaid (buka+tutup tiap blok)
		findings = append(findings, "ada blok ```mermaid yang tidak tampak ditutup")
	}
	return findings
}

// tableDetailPattern: heading section tabel (### Tabel users / ## Tabel ...)
// atau DDL CREATE TABLE.
var tableDetailPattern = regexp.MustCompile(`(?im)^(#{2,4}\s*(tabel|table)\b.*|.*create\s+table\s+\w+)`)

// checkTableDetail: schema wajib memuat detail per tabel (penjelasan
// kolom/tipe/constraint). Kalau tidak ada heading "Tabel X" dan tidak ada
// CREATE TABLE sama sekali, dokumen itu cuma ERD tanpa detail -- gagal
// memenuhi output wajib prompt lld_erd.
func checkTableDetail(doc string) []string {
	if tableDetailPattern.MatchString(doc) {
		return nil
	}
	return []string{"tidak ditemukan section detail per tabel (heading 'Tabel ...' / 'CREATE TABLE') -- penjelasan kolom & constraint adalah output wajib schema"}
}

// endpointPattern: baris spesifikasi endpoint, mis. "POST /api/users" atau
// heading "### POST /users".
var endpointPattern = regexp.MustCompile(`(?im)^(#{2,4}\s*)?(GET|POST|PUT|PATCH|DELETE)\s+/`)

// checkEndpoints: API contracts wajib memuat spesifikasi endpoint dengan
// method + path. Nol match berarti dokumen itu kemungkinan besar omongan
// umum tanpa kontrak konkret.
func checkEndpoints(doc string) []string {
	if endpointPattern.MatchString(doc) {
		return nil
	}
	return []string{"tidak ditemukan endpoint dengan format 'METHOD /path' (mis. 'POST /api/users') -- spesifikasi per endpoint adalah output wajib API contracts"}
}

// phaseHeadingPattern: heading fase di coding plan, mis. "## Fase 1: Setup".
var phaseHeadingPattern = regexp.MustCompile(`(?im)^#{1,6}\s*(fase|phase)\s*\d+`)

// checkPlanPhases: coding plan wajib dipecah per fase. Tanpa heading fase,
// issues agent tidak punya acuan urutan pengerjaan (field "phase" issue
// diambil persis dari sini).
func checkPlanPhases(doc string) []string {
	if phaseHeadingPattern.MatchString(doc) {
		return nil
	}
	return []string{"tidak ditemukan heading fase (mis. '## Fase 1: ...') -- pemecahan per fase adalah struktur wajib coding plan"}
}
