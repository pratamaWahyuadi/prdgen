package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestTokenHarborProvider_Wiring memverifikasi bahwa TokenHarbor provider
// benar-benar mengirim request ke endpoint OpenAI-compatible dengan:
// - Authorization: Bearer <key>
// - model yang di-set lewat env/argumen
// - body chat/completions standar
// dan mem-parse response choices[0].message.content dengan benar.
// Mock server lokal -- tidak ada panggilan ke tokenharbor.ai sungguhan.
func TestTokenHarborProvider_Wiring(t *testing.T) {
	var gotAuth, gotModel string
	var gotPath string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		var body struct {
			Model    string    `json:"model"`
			Messages []Message `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
		}
		gotModel = body.Model
		if len(body.Messages) == 0 || body.Messages[0].Content == "" {
			t.Error("expected messages in request body")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"choices":[{"message":{"content":"ok dari mock"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`))
	}))
	defer srv.Close()

	p := NewTokenHarborProvider("thk_live_test123", "tokenharbor/qwen3-max",
		WithBaseURL(srv.URL+"/v1/chat/completions"),
	)

	resp, err := p.Complete(context.Background(), CompletionRequest{
		SystemPrompt: "sys",
		Messages:     []Message{{Role: "user", Content: "halo"}},
		Temperature:  0.4,
		MaxTokens:    100,
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.Content != "ok dari mock" {
		t.Errorf("unexpected content: %q", resp.Content)
	}
	if gotPath != "/v1/chat/completions" {
		t.Errorf("expected path /v1/chat/completions, got %q", gotPath)
	}
	if gotAuth != "Bearer thk_live_test123" {
		t.Errorf("expected Bearer auth, got %q", gotAuth)
	}
	if gotModel != "tokenharbor/qwen3-max" {
		t.Errorf("expected model tokenharbor/qwen3-max, got %q", gotModel)
	}
	if p.Name() != "deepseek:tokenharbor/qwen3-max" {
		t.Errorf("unexpected provider name %q (catatan: prefix 'deepseek:' karena berbagi klien; acceptable)", p.Name())
	}
}

// TestTokenHarborProvider_RejectsEmptyContent: response sukses tapi content
// kosong (kasus model reasoning habis token) harus jadi error jelas --
// bukan dokumen kosong yang tersimpan diam-diam.
func TestTokenHarborProvider_RejectsEmptyContent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"choices":[{"message":{"content":""},"finish_reason":"length"}],"usage":{"prompt_tokens":1,"completion_tokens":99}}`))
	}))
	defer srv.Close()

	p := NewTokenHarborProvider("thk_live_test123", "tokenharbor/qwen3-max",
		WithBaseURL(srv.URL+"/v1/chat/completions"),
	)
	_, err := p.Complete(context.Background(), CompletionRequest{
		Messages: []Message{{Role: "user", Content: "halo"}},
	})
	if err == nil {
		t.Fatal("expected error for empty content response")
	}
}
