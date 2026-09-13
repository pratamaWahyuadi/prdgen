package main

import (
	"bufio"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pratamaWahyuadi/prdgen/internal/ghissues"
	"github.com/pratamaWahyuadi/prdgen/internal/llm"
	"github.com/pratamaWahyuadi/prdgen/internal/pipeline"
	"github.com/pratamaWahyuadi/prdgen/internal/store"
)

// newTestStore bikin store di temp dir dan mengembalikan cleanup func.
func newTestStore(t *testing.T) (*store.Store, func()) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "proj")
	s, err := store.New(dir)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	return s, func() {}
}

// --- askDeepDiveChoice ---

func TestAskDeepDiveChoice_RepromptsOnUnknownInput(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{"y langsung", "y\n", "y"},
		{"salah ketik lalu y", "x\ny\n", "y"},
		{"salah ketik dua kali lalu n", "zz\n12\nn\n", "n"},
		{"q eksplisit", "q\n", "q"},
		{"yes varian", "YES\n", "y"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			reader := bufio.NewReader(strings.NewReader(c.input))
			if got := askDeepDiveChoice(reader); got != c.want {
				t.Errorf("askDeepDiveChoice(%q) = %q, want %q", c.input, got, c.want)
			}
		})
	}
}

func TestAskDeepDiveChoice_AbortsAfterThreeUnknownInputs(t *testing.T) {
	// 3x input sampah -> q (bukan infinite loop, bukan nebak pilihan).
	reader := bufio.NewReader(strings.NewReader("a\nb\nc\n"))
	if got := askDeepDiveChoice(reader); got != "q" {
		t.Errorf("expected q after 3 unknown inputs, got %q", got)
	}
}

// --- determineStartStage (resume menghormati gate) ---

func TestDetermineStartStage_Fresh(t *testing.T) {
	s, _ := newTestStore(t)
	if got := determineStartStage(s); got.String() != "discovery" {
		t.Errorf("fresh project should start at discovery, got %s", got)
	}
}

func TestDetermineStartStage_AfterQA_GoesToBrief(t *testing.T) {
	s, _ := newTestStore(t)
	mustSave(t, s, store.FileIdea, "ide")
	mustSave(t, s, store.FileDiscoveryQA, "Q & A")
	if got := determineStartStage(s); got.String() != "discovery_brief" {
		t.Errorf("after QA should resume at discovery_brief, got %s", got)
	}
}

func TestDetermineStartStage_AtGate_WhenNoDeepDiveOrDefaults(t *testing.T) {
	s, _ := newTestStore(t)
	mustSave(t, s, store.FileIdea, "ide")
	mustSave(t, s, store.FileDiscoveryQA, "Q & A")
	mustSave(t, s, store.FileProductBrief, "# Brief")
	if got := determineStartStage(s); got.String() != "discovery_gate" {
		t.Errorf("brief done without deep-dive/defaults should resume at gate, got %s", got)
	}
}

func TestDetermineStartStage_SkippedDeepDive_ResumesAtSecurity(t *testing.T) {
	// User skip deep-dive: defaults.yaml ada, 01c tidak -> langsung security,
	// TIDAK memaksa user menjawab deep-dive lagi.
	s, _ := newTestStore(t)
	mustSave(t, s, store.FileIdea, "ide")
	mustSave(t, s, store.FileDiscoveryQA, "Q & A")
	mustSave(t, s, store.FileProductBrief, "# Brief")
	mustSave(t, s, store.FileDefaultsYAML, "stack:\n  language: go")
	if got := determineStartStage(s); got.String() != "security" {
		t.Errorf("skip path (defaults.yaml present) should resume at security, got %s", got)
	}
}

func TestDetermineStartStage_DeepDiveDone_ResumesAtSecurity(t *testing.T) {
	s, _ := newTestStore(t)
	mustSave(t, s, store.FileIdea, "ide")
	mustSave(t, s, store.FileDiscoveryQA, "Q & A")
	mustSave(t, s, store.FileProductBrief, "# Brief")
	mustSave(t, s, store.FileDeepDiveQA, "driver: pgx/v5")
	if got := determineStartStage(s); got.String() != "security" {
		t.Errorf("deep-dive done should resume at security, got %s", got)
	}
}

// --- writeDefaultsFromBrief ---

func TestWriteDefaultsFromBrief_SavesYAML(t *testing.T) {
	s, _ := newTestStore(t)
	mock := &llm.MockProvider{Responses: []string{"stack:\n  language: Go\n  database: PostgreSQL\n"}}
	r := newTestRunnerWithMock(t, mock)

	if err := writeDefaultsFromBrief(context.Background(), r, s, "ide app", "# Brief"); err != nil {
		t.Fatalf("writeDefaultsFromBrief: %v", err)
	}
	if !s.IsComplete(store.FileDefaultsYAML) {
		t.Fatal("defaults.yaml should be saved")
	}
	content, err := s.Load(store.FileDefaultsYAML)
	if err != nil {
		t.Fatalf("load defaults: %v", err)
	}
	if !strings.Contains(content, "language: Go") {
		t.Errorf("expected LLM output saved as-is, got: %s", content)
	}
}

func TestWriteDefaultsFromBrief_RejectsEmptyOutput(t *testing.T) {
	// Store.Save menolak konten kosong; error harus propagate, bukan file
	// defaults.yaml kosong yang dianggap "sudah lewat gate" oleh resume.
	s, _ := newTestStore(t)
	mock := &llm.MockProvider{Responses: []string{""}}
	r := newTestRunnerWithMock(t, mock)

	if err := writeDefaultsFromBrief(context.Background(), r, s, "ide", "# Brief"); err == nil {
		t.Fatal("expected error for empty LLM output (store refuses empty save)")
	}
	if s.IsComplete(store.FileDefaultsYAML) {
		t.Error("defaults.yaml must NOT exist after failed generation")
	}
}

// newTestRunnerWithMock bikin pipeline.Runner dengan mock provider -- tidak
// butuh API key, semua panggilan LLM dijawab dari Responses.
func newTestRunnerWithMock(t *testing.T, mock *llm.MockProvider) *pipeline.Runner {
	t.Helper()
	return &pipeline.Runner{Provider: mock}
}

// --- findLoggedTitlesMissingFromDraft ---

func TestFindLoggedTitlesMissingFromDraft(t *testing.T) {
	logged := map[string]bool{"Issue Lama": true, "Issue Sama": true}
	issues := []ghissues.Issue{
		{Title: "Issue Sama"},
		{Title: "Issue Baru"},
	}

	got := findLoggedTitlesMissingFromDraft(logged, issues)
	if len(got) != 1 || got[0] != "Issue Lama" {
		t.Errorf("expected [Issue Lama], got %v", got)
	}
}

func TestFindLoggedTitlesMissingFromDraft_AllMatch(t *testing.T) {
	logged := map[string]bool{"A": true}
	got := findLoggedTitlesMissingFromDraft(logged, []ghissues.Issue{{Title: "A"}})
	if len(got) != 0 {
		t.Errorf("expected no missing titles, got %v", got)
	}
}

// --- helpers ---

func mustSave(t *testing.T, s *store.Store, file, content string) {
	t.Helper()
	if _, err := s.Save(file, content); err != nil {
		t.Fatalf("save %s: %v", file, err)
	}
}
