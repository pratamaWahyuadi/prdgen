package pipeline

import (
	"context"
	"strings"
	"testing"

	"github.com/pratamaWahyuadi/prdgen/internal/llm"
)

func TestKnowledgeSection_Empty(t *testing.T) {
	r := &Runner{}
	if got := r.knowledgeSection(); got != "" {
		t.Errorf("expected empty section without knowledge, got: %q", got)
	}
}

func TestKnowledgeSection_ContainsKnowledgeAndPrecedenceRule(t *testing.T) {
	r := &Runner{Knowledge: "Zitadel v4: ROPC tidak ada. Web app selalu confidential."}
	got := r.knowledgeSection()
	for _, want := range []string{
		"Zitadel v4: ROPC tidak ada",
		"MENANG atas pengetahuan umum",
		"REFERENSI TEKNOLOGI PENGGUNA",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("knowledgeSection should contain %q, got: %.200s", want, got)
		}
	}
}

func TestComplete_InjectsKnowledgeIntoEveryCall(t *testing.T) {
	// Knowledge harus masuk ke SETIAP panggilan model via complete() --
	// choke point tunggal. Test pakai dua stage berbeda (discovery & erd)
	// untuk membuktikan keduanya ke-inject, bukan cuma stage tertentu.
	mock := &llm.MockProvider{Responses: []string{"a", "b"}}
	r := &Runner{Provider: mock, Knowledge: "GOTCHA: ROPC absent in Zitadel v4"}

	if _, err := r.RunDiscovery(context.Background(), "ide"); err != nil {
		t.Fatalf("RunDiscovery: %v", err)
	}
	if !strings.Contains(mock.LastRequest.Messages[0].Content, "ROPC absent in Zitadel v4") {
		t.Errorf("RunDiscovery request harus berisi knowledge, got: %.200s", mock.LastRequest.Messages[0].Content)
	}

	if _, err := r.RunLLDErd(context.Background(), "prd"); err != nil {
		t.Fatalf("RunLLDErd: %v", err)
	}
	if !strings.Contains(mock.LastRequest.Messages[0].Content, "ROPC absent in Zitadel v4") {
		t.Errorf("RunLLDErd request harus berisi knowledge, got: %.200s", mock.LastRequest.Messages[0].Content)
	}
	// Knowledge ditaruh SETELAH konteks task (posisi = presedensi).
	if !strings.HasSuffix(mock.LastRequest.Messages[0].Content, "ROPC absent in Zitadel v4") {
		t.Errorf("knowledge harus berada di akhir user content (setelah konteks task)")
	}
}

func TestComplete_NoKnowledge_NoInjection(t *testing.T) {
	// Tanpa knowledge, request tidak boleh berisi header injection sama
	// sekali -- jangan kirim blok kosong yang boros token.
	mock := &llm.MockProvider{Responses: []string{"a"}}
	r := &Runner{Provider: mock}

	if _, err := r.RunDiscovery(context.Background(), "ide"); err != nil {
		t.Fatalf("RunDiscovery: %v", err)
	}
	if strings.Contains(mock.LastRequest.Messages[0].Content, "REFERENSI TEKNOLOGI PENGGUNA") {
		t.Errorf("tanpa knowledge tidak boleh ada header injection")
	}
}
