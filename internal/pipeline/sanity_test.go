package pipeline

import (
	"context"
	"strings"
	"testing"

	"prdgen/internal/llm"
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
