package ghissues

import (
	"strings"
	"testing"
)

func TestValidateDraft_PhaseMatchesPlan(t *testing.T) {
	t.Parallel()

	plan := "## Fase 1: Setup & DB Migrations\nsetup db\n\n## Fase 2: Core Domain Logic\ndomain"
	issues := []Issue{
		{Title: "Setup DB", Body: "buat pool postgres di internal/db/db.go", Labels: []string{"phase-1"}, Phase: "Fase 1: Setup & DB Migrations"},
		{Title: "Domain logic", Body: "implement domain", Labels: []string{"phase-2"}, Phase: "Fase 2 Versi Karangan Agent"}, // salah: tidak ada di plan
	}

	errs := ValidateDraft(issues, plan)
	if len(errs) != 1 {
		t.Fatalf("expected exactly 1 finding (wrong phase on issue #2), got %d: %v", len(errs), errs)
	}
	if errs[0].Issue != 2 {
		t.Errorf("expected finding on issue #2, got #%d", errs[0].Issue)
	}
	if errs[0].Rule != "phase-matches-plan" {
		t.Errorf("expected rule phase-matches-plan, got %s", errs[0].Rule)
	}
	if !strings.Contains(errs[0].Msg, "Fase 1: Setup & DB Migrations") {
		t.Errorf("expected suggestion to list known phases, got: %s", errs[0].Msg)
	}
}

func TestValidateDraft_PhaseOK_NoFindings(t *testing.T) {
	t.Parallel()

	plan := "## Fase 1: Setup\nsetup\n\n## Fase 2: API\napi"
	issues := []Issue{
		{Title: "A", Body: "setup project", Phase: "Fase 1: Setup"},
		{Title: "B", Body: "buat endpoint", Phase: "Fase 2: API"},
	}

	if errs := ValidateDraft(issues, plan); len(errs) != 0 {
		t.Fatalf("expected no findings, got: %v", errs)
	}
}

func TestValidateDraft_DBWithoutConnSource(t *testing.T) {
	t.Parallel()

	// Plan mengunci file sumber koneksi `internal/db/db.go`.
	plan := "# Fase 1: Setup & DB Migrations\n\nStep: Setup Koneksi Database (Single Source of Truth) -- buat pool di `internal/db/db.go` (pgx/v5)."
	issues := []Issue{
		// Body menyentuh database tapi tidak menyebut file koneksi -> temuan.
		{Title: "Repo users", Body: "buat repository untuk tabel users dengan query SQL", Phase: "Fase 1: Setup & DB Migrations"},
		// Body menyebut file koneksi -> tidak ada temuan.
		{Title: "Repo events", Body: "repository events, reuse pool dari internal/db/db.go", Phase: "Fase 1: Setup & DB Migrations"},
	}

	errs := ValidateDraft(issues, plan)
	if len(errs) != 1 {
		t.Fatalf("expected exactly 1 finding (issue #1 missing conn source), got %d: %v", len(errs), errs)
	}
	if errs[0].Issue != 1 {
		t.Errorf("expected finding on issue #1, got #%d", errs[0].Issue)
	}
	if errs[0].Rule != "db-conn-source-cited" {
		t.Errorf("expected rule db-conn-source-cited, got %s", errs[0].Rule)
	}
	if !strings.Contains(errs[0].Msg, "internal/db/db.go") {
		t.Errorf("expected message to name the locked conn file, got: %s", errs[0].Msg)
	}
}

func TestValidateDraft_EmptyPlan_NoRulesFire(t *testing.T) {
	t.Parallel()

	// Plan kosong: tidak ada fase maupun file koneksi yang bisa dipakai
	// sebagai acuan -- validator tidak boleh mengarang aturan dari kekosongan.
	issues := []Issue{
		{Title: "A", Body: "database koneksi", Phase: "Fase 1"},
	}
	if errs := ValidateDraft(issues, ""); len(errs) != 0 {
		t.Fatalf("expected no findings with empty plan, got: %v", errs)
	}
}
