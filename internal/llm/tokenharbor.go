package llm

import (
	"net/http"
	"time"
)

// tokenHarborBaseURL: TokenHarbor memakai protokol OpenAI-compatible
// (endpoint /v1/chat/completions, bentuk request/response identik dengan
// DeepSeek), jadi implementasi ini cukup mendelegasikan ke
// DeepSeekProvider dengan baseURL berbeda -- tanpa duplikasi wire-format.
const tokenHarborBaseURL = "https://tokenharbor.ai/v1/chat/completions"

// NewTokenHarborProvider membuat provider TokenHarbor (tokenharbor.ai)
// lewat protokol OpenAI-compatible yang sama dengan DeepSeek.
func NewTokenHarborProvider(apiKey, model string, opts ...DeepSeekOption) *DeepSeekProvider {
	// Timeout diperbesar: model di TokenHarbor (mis. qwen3-max) bisa jadi
	// reasoning berat untuk dokumen planning yang panjang.
	base := []DeepSeekOption{
		WithBaseURL(tokenHarborBaseURL),
		WithHTTPClient(&http.Client{Timeout: 1200 * time.Second}),
	}
	return NewDeepSeekProvider(apiKey, model, append(base, opts...)...)
}
