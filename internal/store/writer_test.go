package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadKnowledge_NoDir(t *testing.T) {
	s, err := New(filepath.Join(t.TempDir(), "proj"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.LoadKnowledge()
	if err != nil {
		t.Fatalf("no knowledge dir must not error: %v", err)
	}
	if got != "" {
		t.Errorf("expected empty, got: %q", got)
	}
}

func TestLoadKnowledge_MergesMdAndTxtSorted(t *testing.T) {
	s, err := New(filepath.Join(t.TempDir(), "proj"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(s.Dir, "knowledge"), 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(s.Dir, "knowledge", "b-zitadel.md"), []byte("ROPC absent"), 0o644)
	os.WriteFile(filepath.Join(s.Dir, "knowledge", "a-r2.txt"), []byte("R2 notes"), 0o644)
	os.WriteFile(filepath.Join(s.Dir, "knowledge", "ignore.json"), []byte("{}"), 0o644) // bukan .md/.txt -> skip
	os.WriteFile(filepath.Join(s.Dir, "knowledge", "empty.md"), []byte(""), 0o644)    // kosong -> skip

	got, err := s.LoadKnowledge()
	if err != nil {
		t.Fatalf("LoadKnowledge: %v", err)
	}
	for _, want := range []string{"a-r2.txt", "b-zitadel.md", "ROPC absent", "R2 notes"} {
		if !strings.Contains(got, want) {
			t.Errorf("expected merged content to contain %q, got: %.200s", want, got)
		}
	}
	if strings.Contains(got, "ignore.json") || strings.Contains(got, "empty.md") {
		t.Errorf("non-md/txt and empty files must be skipped, got: %.200s", got)
	}
	// Urut abjad: a-r2.txt harus muncul sebelum b-zitadel.md.
	if strings.Index(got, "a-r2.txt") > strings.Index(got, "b-zitadel.md") {
		t.Errorf("files must be merged in sorted order")
	}
}
