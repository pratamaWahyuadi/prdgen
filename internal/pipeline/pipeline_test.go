package pipeline

import (
	"context"
	"strings"
	"testing"

	"prdgen/internal/llm"
)

func TestRunDiscovery(t *testing.T) {
	mock := &llm.MockProvider{Responses: []string{"1. Skala berapa user?\n2. Timeline?"}}
	r := &Runner{Provider: mock}

	out, err := r.RunDiscovery(context.Background(), "aplikasi todo list")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "Skala berapa user") {
		t.Errorf("expected discovery questions in output, got: %s", out)
	}
	if !strings.Contains(mock.LastRequest.Messages[0].Content, "aplikasi todo list") {
		t.Errorf("expected raw idea to be passed to model, got: %s", mock.LastRequest.Messages[0].Content)
	}
}

func TestRunSecurity_InjectsDiscoveryContext(t *testing.T) {
	mock := &llm.MockProvider{Responses: []string{"Threat Report: ..."}}
	r := &Runner{Provider: mock}

	_, err := r.RunSecurity(context.Background(), "aplikasi e-commerce", "Q: skala? A: 10k user")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	content := mock.LastRequest.Messages[0].Content
	if !strings.Contains(content, "aplikasi e-commerce") || !strings.Contains(content, "10k user") {
		t.Errorf("expected both raw idea and discovery QA in context, got: %s", content)
	}
}

func TestRunPRD_InjectsAllContext(t *testing.T) {
	mock := &llm.MockProvider{Responses: []string{"# PRD\n..."}}
	r := &Runner{Provider: mock}

	out, err := r.RunPRD(context.Background(), "idea", "qa", "threat report XYZ", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "# PRD\n..." {
		t.Errorf("unexpected output: %s", out)
	}
	content := mock.LastRequest.Messages[0].Content
	if !strings.Contains(content, "threat report XYZ") {
		t.Errorf("expected threat report to be injected into PRD context, got: %s", content)
	}
}

func TestRunLLDErd(t *testing.T) {
	mock := &llm.MockProvider{Responses: []string{"erd mermaid here"}}
	r := &Runner{Provider: mock}

	out, err := r.RunLLDErd(context.Background(), "some prd content")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "erd mermaid here" {
		t.Errorf("unexpected output: %s", out)
	}
}

func TestRunLLDApi_UsesSchemaFromPreviousStage(t *testing.T) {
	mock := &llm.MockProvider{Responses: []string{"api contracts here"}}
	r := &Runner{Provider: mock}

	_, err := r.RunLLDApi(context.Background(), "prd content", "CREATE TABLE users (...)")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	content := mock.LastRequest.Messages[0].Content
	if !strings.Contains(content, "CREATE TABLE users") {
		t.Errorf("expected schema from ERD stage to be passed in, got: %s", content)
	}
}

func TestRunLLDPlan_UsesAllPreviousOutputs(t *testing.T) {
	mock := &llm.MockProvider{Responses: []string{"plan here"}}
	r := &Runner{Provider: mock}

	_, err := r.RunLLDPlan(context.Background(), "prd", "schema", "api contracts")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	content := mock.LastRequest.Messages[0].Content
	for _, want := range []string{"prd", "schema", "api contracts"} {
		if !strings.Contains(content, want) {
			t.Errorf("expected %q in plan context, got: %s", want, content)
		}
	}
}

func TestRunPRD_InjectsDeepDiveContext(t *testing.T) {
	mock := &llm.MockProvider{Responses: []string{"# PRD\n..."}}
	r := &Runner{Provider: mock}

	_, err := r.RunPRD(context.Background(), "idea", "qa", "threat", "driver: pgx/v5 native pool")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	content := mock.LastRequest.Messages[0].Content
	if !strings.Contains(content, "driver: pgx/v5 native pool") {
		t.Errorf("expected deep-dive context injected into PRD, got: %s", content)
	}
	if !strings.Contains(content, "Asumsi Teknis") {
		t.Errorf("expected Asumsi Teknis instruction in PRD context, got: %s", content)
	}
}

func TestRunDiscoveryBrief_InjectsQA(t *testing.T) {
	mock := &llm.MockProvider{Responses: []string{"# Product Brief\n..."}}
	r := &Runner{Provider: mock}

	_, err := r.RunDiscoveryBrief(context.Background(), "ide app", "Q: skala? A: 10k user")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	content := mock.LastRequest.Messages[0].Content
	if !strings.Contains(content, "10k user") {
		t.Errorf("expected discovery QA in brief context, got: %s", content)
	}
}

func TestRunDiscoveryDeep_InjectsBrief(t *testing.T) {
	mock := &llm.MockProvider{Responses: []string{"1. Driver DB apa?"}}
	r := &Runner{Provider: mock}

	_, err := r.RunDiscoveryDeep(context.Background(), "ide app", "# Product Brief\nstack: Go + Postgres")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	content := mock.LastRequest.Messages[0].Content
	if !strings.Contains(content, "Go + Postgres") {
		t.Errorf("expected product brief in deep-dive context, got: %s", content)
	}
}

func TestRunGenerateDefaults_InjectsBrief(t *testing.T) {
	mock := &llm.MockProvider{Responses: []string{"stack:\n  language: Go"}}
	r := &Runner{Provider: mock}

	_, err := r.RunGenerateDefaults(context.Background(), "ide app", "# Product Brief\nfamiliarity: lib/pq")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	content := mock.LastRequest.Messages[0].Content
	if !strings.Contains(content, "lib/pq") {
		t.Errorf("expected product brief (familiarity) in defaults context, got: %s", content)
	}
}

func TestRunDiscovery_PropagatesProviderError(t *testing.T) {
	mock := &llm.MockProvider{Err: errTest{}}
	r := &Runner{Provider: mock}

	_, err := r.RunDiscovery(context.Background(), "idea")
	if err == nil {
		t.Fatal("expected error to propagate from provider")
	}
}

func TestRunValidatePRD_InjectsDiscoveryAndPRD(t *testing.T) {
	mock := &llm.MockProvider{Responses: []string{"PRD konsisten penuh dengan hasil discovery."}}
	r := &Runner{Provider: mock}

	out, err := r.RunValidatePRD(context.Background(), "Q: bahasa? A: Go", "# PRD\nTech Stack: Go")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "PRD konsisten penuh dengan hasil discovery." {
		t.Errorf("unexpected output: %s", out)
	}
	content := mock.LastRequest.Messages[0].Content
	if !strings.Contains(content, "Q: bahasa? A: Go") {
		t.Errorf("expected discovery QA in context, got: %s", content)
	}
	if !strings.Contains(content, "Tech Stack: Go") {
		t.Errorf("expected PRD content in context, got: %s", content)
	}
}

func TestRunValidatePRD_PropagatesProviderError(t *testing.T) {
	mock := &llm.MockProvider{Err: errTest{}}
	r := &Runner{Provider: mock}

	_, err := r.RunValidatePRD(context.Background(), "qa", "prd")
	if err == nil {
		t.Fatal("expected error to propagate from provider")
	}
}

func TestRunValidateLLD_InjectsAllFourDocuments(t *testing.T) {
	mock := &llm.MockProvider{Responses: []string{"LLD konsisten penuh dengan tech stack yang diputuskan PRD."}}
	r := &Runner{Provider: mock}

	out, err := r.RunValidateLLD(context.Background(), "prd content Go", "schema content", "api content", "plan content Go")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "LLD konsisten penuh dengan tech stack yang diputuskan PRD." {
		t.Errorf("unexpected output: %s", out)
	}
	content := mock.LastRequest.Messages[0].Content
	for _, want := range []string{"prd content Go", "schema content", "api content", "plan content Go"} {
		if !strings.Contains(content, want) {
			t.Errorf("expected %q in validation context, got: %s", want, content)
		}
	}
}

func TestRunValidateLLD_PropagatesProviderError(t *testing.T) {
	mock := &llm.MockProvider{Err: errTest{}}
	r := &Runner{Provider: mock}

	_, err := r.RunValidateLLD(context.Background(), "prd", "schema", "api", "plan")
	if err == nil {
		t.Fatal("expected error to propagate from provider")
	}
}

func TestRunGenerateIssues_InjectsAllContext(t *testing.T) {
	mock := &llm.MockProvider{Responses: []string{`[{"title":"Setup DB","body":"...","labels":["phase-1"],"phase":"Fase 1"}]`}}
	r := &Runner{Provider: mock}

	out, err := r.RunGenerateIssues(context.Background(), "prd content", "schema content", "api content", "plan content")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "Setup DB") {
		t.Errorf("unexpected output: %s", out)
	}
	content := mock.LastRequest.Messages[0].Content
	for _, want := range []string{"prd content", "schema content", "api content", "plan content"} {
		if !strings.Contains(content, want) {
			t.Errorf("expected %q in issues context, got: %s", want, content)
		}
	}
}

func TestRunGenerateIssues_PropagatesProviderError(t *testing.T) {
	mock := &llm.MockProvider{Err: errTest{}}
	r := &Runner{Provider: mock}

	_, err := r.RunGenerateIssues(context.Background(), "prd", "schema", "api", "plan")
	if err == nil {
		t.Fatal("expected error to propagate from provider")
	}
}

func TestRunReviseIssue_InjectsFeedbackAndCurrentIssue(t *testing.T) {
	mock := &llm.MockProvider{Responses: []string{`{"title":"Setup DB (revised)","body":"...","labels":["phase-1"],"phase":"Fase 1"}`}}
	r := &Runner{Provider: mock}

	out, err := r.RunReviseIssue(context.Background(), "schema", "api", "plan", `{"title":"Setup DB"}`, "tambahin test rollback")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "revised") {
		t.Errorf("unexpected output: %s", out)
	}
	content := mock.LastRequest.Messages[0].Content
	if !strings.Contains(content, "tambahin test rollback") {
		t.Errorf("expected feedback in context, got: %s", content)
	}
	if !strings.Contains(content, `"title":"Setup DB"`) {
		t.Errorf("expected current issue JSON in context, got: %s", content)
	}
}

func TestRunReviseDocument_InjectsFeedbackAndContext(t *testing.T) {
	mock := &llm.MockProvider{Responses: []string{"# PRD (revised)\n..."}}
	r := &Runner{Provider: mock}

	out, err := r.RunReviseDocument(context.Background(), "# PRD\n...", "tambahin section auth", "Discovery:\nQ&A...")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "revised") {
		t.Errorf("unexpected output: %s", out)
	}
	content := mock.LastRequest.Messages[0].Content
	if !strings.Contains(content, "tambahin section auth") {
		t.Errorf("expected feedback in context, got: %s", content)
	}
	if !strings.Contains(content, "Discovery:\nQ&A...") {
		t.Errorf("expected doc context in context, got: %s", content)
	}
}

func TestRunReviseDocument_PropagatesProviderError(t *testing.T) {
	mock := &llm.MockProvider{Err: errTest{}}
	r := &Runner{Provider: mock}

	_, err := r.RunReviseDocument(context.Background(), "doc", "feedback", "context")
	if err == nil {
		t.Fatal("expected error to propagate from provider")
	}
}

type errTest struct{}

func (errTest) Error() string { return "mock provider error" }
