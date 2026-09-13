package pipeline

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/pratamaWahyuadi/prdgen/internal/llm"
)

func TestIsTruncated(t *testing.T) {
	t.Parallel()

	cases := []struct {
		reason string
		want   bool
	}{
		{"length", true},     // DeepSeek / OpenAI-compatible
		{"LENGTH", true},     // case-insensitive
		{" length ", true},   // toleran whitespace
		{"MAX_TOKENS", true}, // Gemini
		{"max_tokens", true}, // Gemini lowercase
		{"stop", false},      // selesai normal
		{"", false},          // tidak dilaporkan provider (mock / legacy)
		{"content_filter", false},
	}
	for _, c := range cases {
		if got := IsTruncated(c.reason); got != c.want {
			t.Errorf("IsTruncated(%q) = %v, want %v", c.reason, got, c.want)
		}
	}
}

func TestComplete_RejectsTruncatedOutput(t *testing.T) {
	mock := &llm.MockProvider{Responses: []string{"dokumen setengah jadi..."}, FinishReason: "length"}
	r := &Runner{Provider: mock}

	_, err := r.complete(context.Background(), "sys", "user content")
	if err == nil {
		t.Fatal("expected error for truncated finish_reason=length, got nil")
	}
	if !strings.Contains(err.Error(), "terpotong") {
		t.Errorf("expected truncation message, got: %v", err)
	}
}

func TestComplete_AllowsNormalStop(t *testing.T) {
	mock := &llm.MockProvider{Responses: []string{"dokumen lengkap."}, FinishReason: "stop"}
	r := &Runner{Provider: mock}

	out, err := r.complete(context.Background(), "sys", "user content")
	if err != nil {
		t.Fatalf("unexpected error for finish_reason=stop: %v", err)
	}
	if out != "dokumen lengkap." {
		t.Errorf("unexpected output: %s", out)
	}
}

func TestSanityCheck_EmptyDoc(t *testing.T) {
	findings := SanityCheck("", DocOther)
	if len(findings) != 1 || findings[0] != "dokumen kosong total" {
		t.Fatalf("expected single empty-doc finding, got: %v", findings)
	}
}

func TestSanityCheck_UnclosedFence(t *testing.T) {
	doc := "# Judul\n\n```mermaid\nerDiagram\n  users ||--o{ events : has\n" // fence tidak ditutup
	findings := SanityCheck(doc, DocSchema)
	found := false
	for _, f := range findings {
		if strings.Contains(f, "tidak ditutup") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected unclosed-fence finding, got: %v", findings)
	}
}

func TestSanityCheck_SchemaWithoutMermaid(t *testing.T) {
	doc := "# Schema\ntabel users punya kolom id.\n\nCREATE TABLE users (id int);"
	findings := SanityCheck(doc, DocSchema)
	found := false
	for _, f := range findings {
		if strings.Contains(f, "mermaid") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected missing-mermaid finding, got: %v", findings)
	}
}

func TestSanityCheck_SchemaWithoutTableDetail(t *testing.T) {
	doc := "# Schema\n\n```mermaid\nerDiagram\n```\n\nTabel di atas.\n" // fence ditutup
	findings := SanityCheck(doc, DocSchema)
	found := false
	for _, f := range findings {
		if strings.Contains(f, "detail per tabel") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected missing-table-detail finding, got: %v", findings)
	}
}

func TestSanityCheck_APIWithoutEndpoints(t *testing.T) {
	doc := "# API Contracts\nDokumen ini menjelaskan API secara umum tanpa contoh konkret."
	findings := SanityCheck(doc, DocAPI)
	found := false
	for _, f := range findings {
		if strings.Contains(f, "endpoint") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected missing-endpoint finding, got: %v", findings)
	}
}

func TestSanityCheck_APIEndpointsInRealWorldFormats(t *testing.T) {
	// Regression (TikTok-clone planning): format output LLM nyata --
	// heading ber-backtick dan baris tabel -- dulu ber-flag FALSE
	// "tidak ditemukan endpoint" karena regex lama di-anchor ^heading
	// tanpa backtick. Semua varian di bawah wajib lolos tanpa temuan.
	docs := []string{
		// heading ber-backtick (format yang kena bug)
		"# API\n\n#### `GET /api/users/me`\n\nprose.\n",
		// heading plain
		"# API\n\n#### POST /api/videos\n\nprose.\n",
		// heading 2-6 hash
		"# API\n\n###### `DELETE /api/comments/:id`\n",
		// baris tabel
		"# API\n\n| METHOD / Path | Auth |\n|---|---|\n| `POST /api/videos/upload-intent` | wajib |\n",
		// inline bold + backtick
		"# API\n\nPanggil **`POST /api/videos/confirm`** setelah upload.\n",
		// method di awal dokumen (posisi 0)
		"`GET /api/health` — liveness probe.\n",
	}
	for i, doc := range docs {
		findings := SanityCheck(doc, DocAPI)
		for _, f := range findings {
			if strings.Contains(f, "endpoint") {
				t.Errorf("doc %d: valid endpoint format must not trigger finding: %s (doc: %.80s)", i, f, doc)
			}
		}
	}
}

func TestSanityCheck_RealAPIContractDoc(t *testing.T) {
	// Verifikasi fix bug terhadap dokumen produksi nyata yang memicunya:
	// API contract TikTok-clone (24 endpoint, format heading ber-backtick
	// + tabel) dulu memicu FALSE "tidak ditemukan endpoint".
	// Skip kalau dokumen tidak ada di mesin ini (mis. CI).
	const realDoc = "/home/pratama/prdgen_testing/planning/04_api_contracts.md"
	b, err := os.ReadFile(realDoc)
	if err != nil {
		t.Skipf("dokumen nyata tidak tersedia di mesin ini: %v", err)
	}
	findings := SanityCheck(string(b), DocAPI)
	for _, f := range findings {
		if strings.Contains(f, "endpoint") {
			t.Errorf("dokumen nyata (24 endpoint) tidak boleh memicu temuan endpoint: %s", f)
		}
	}
}

func TestSanityCheck_PlanWithoutPhases(t *testing.T) {
	doc := "# Coding Plan\nLangkah-langkah: bikin project, lalu domain, lalu API. Selesai."
	findings := SanityCheck(doc, DocPlan)
	found := false
	for _, f := range findings {
		if strings.Contains(f, "fase") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected missing-phase finding, got: %v", findings)
	}
}

func TestSanityCheck_GoodDocs_NoFindings(t *testing.T) {
	doc := "# Schema\n\n```mermaid\nerDiagram\n```\n\n## Tabel users\n\n| Kolom | Tipe |\n|---|---|\n| id | uuid |\n\nPenjelasan tabel selesai."
	if findings := SanityCheck(doc, DocSchema); len(findings) != 0 {
		t.Errorf("expected no findings for good schema, got: %v", findings)
	}
}

func TestSanityCheck_PRDMissingRequiredSections(t *testing.T) {
	// PRD tanpa tiga section kontrak (driver, instance terbagi, asumsi
	// teknis) -> 3 temuan, masing-masing menyebut section yang hilang.
	doc := "# PRD Aplikasi\n\n## Overview\nAplikasi todo list sederhana.\n\n## Personas\nUser biasa.\n\nSelesai dan tuntas."
	findings := SanityCheck(doc, DocPRD)
	if len(findings) != 3 {
		t.Fatalf("expected 3 findings (missing driver/instance/asumsi sections), got %d: %v", len(findings), findings)
	}
	var driver, shared, asumsi bool
	for _, f := range findings {
		if strings.Contains(f, "Koneksi & Driver Database") {
			driver = true
		}
		if strings.Contains(f, "Instance Terbagi Lain") {
			shared = true
		}
		if strings.Contains(f, "Asumsi Teknis") {
			asumsi = true
		}
	}
	if !driver || !shared || !asumsi {
		t.Errorf("expected all 3 specific section findings, got: %v", findings)
	}
}

func TestSanityCheck_PRDAllSectionsPresent(t *testing.T) {
	doc := "# PRD\n\n" +
		"## 7. Tech Stack & Justifikasi\n\n### Koneksi & Driver Database\npgx/v5 native pool.\n\n" +
		"### Instance Terbagi Lain\nredis: go-redis v9 single client.\n\n" +
		"## 7.5 Asumsi Teknis\n| Keputusan | Nilai | Status | Basis |\n|---|---|---|---|\n| driver | pgx/v5 | [ASSUMED] | default umum |\n\nSelesai."
	findings := SanityCheck(doc, DocPRD)
	// Section lengkap -> tidak boleh ada temuan dari checkPRDRequiredSections.
	for _, f := range findings {
		if strings.Contains(f, "Koneksi & Driver") || strings.Contains(f, "Instance Terbagi") || strings.Contains(f, "Asumsi Teknis") {
			t.Errorf("unexpected section-missing finding while all sections present: %s", f)
		}
	}
}
