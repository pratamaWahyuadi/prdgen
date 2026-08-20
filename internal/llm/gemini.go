package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const defaultGeminiBaseURL = "https://generativelanguage.googleapis.com/v1beta/models"

// GeminiProvider implements Provider lewat Gemini API (Google AI Studio /
// generativelanguage.googleapis.com), pakai endpoint generateContent standar
// (bukan streaming), supaya perilakunya sama seperti DeepSeekProvider:
// satu request -> satu response lengkap.
type GeminiProvider struct {
	apiKey     string
	baseURL    string
	model      string
	httpClient *http.Client
}

type GeminiOption func(*GeminiProvider)

func WithGeminiBaseURL(url string) GeminiOption {
	return func(p *GeminiProvider) { p.baseURL = url }
}

func WithGeminiHTTPClient(c *http.Client) GeminiOption {
	return func(p *GeminiProvider) { p.httpClient = c }
}

func NewGeminiProvider(apiKey, model string, opts ...GeminiOption) *GeminiProvider {
	p := &GeminiProvider{
		apiKey:  apiKey,
		baseURL: defaultGeminiBaseURL,
		model:   model,
		httpClient: &http.Client{
			Timeout: 1200 * time.Second,
		},
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

func (p *GeminiProvider) Name() string { return "gemini:" + p.model }

type geminiPart struct {
	Text string `json:"text"`
}

type geminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []geminiPart `json:"parts"`
}

type geminiSystemInstruction struct {
	Parts []geminiPart `json:"parts"`
}

type geminiGenerationConfig struct {
	Temperature     float64 `json:"temperature,omitempty"`
	MaxOutputTokens int     `json:"maxOutputTokens,omitempty"`
}

type geminiRequest struct {
	SystemInstruction *geminiSystemInstruction `json:"system_instruction,omitempty"`
	Contents          []geminiContent          `json:"contents"`
	GenerationConfig  *geminiGenerationConfig  `json:"generationConfig,omitempty"`
}

type geminiResponse struct {
	Candidates []struct {
		Content struct {
			Parts []geminiPart `json:"parts"`
			Role  string       `json:"role"`
		} `json:"content"`
		FinishReason string `json:"finishReason"`
	} `json:"candidates"`
	UsageMetadata struct {
		PromptTokenCount     int `json:"promptTokenCount"`
		CandidatesTokenCount int `json:"candidatesTokenCount"`
	} `json:"usageMetadata"`
	Error *struct {
		Message string `json:"message"`
		Status  string `json:"status"`
	} `json:"error,omitempty"`
}

// geminiRole memetakan role internal ("user"/"assistant") ke role yang
// dikenal Gemini ("user"/"model"). Role lain (mis. "system", walau
// harusnya sudah ditangani lewat SystemPrompt, bukan lewat Messages)
// di-fallback ke "user" supaya request tetap valid daripada gagal total.
func geminiRole(role string) string {
	if role == "assistant" || role == "model" {
		return "model"
	}
	return "user"
}

func (p *GeminiProvider) Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error) {
	contents := make([]geminiContent, 0, len(req.Messages))
	for _, m := range req.Messages {
		contents = append(contents, geminiContent{
			Role:  geminiRole(m.Role),
			Parts: []geminiPart{{Text: m.Content}},
		})
	}

	body := geminiRequest{
		Contents: contents,
	}
	if req.SystemPrompt != "" {
		body.SystemInstruction = &geminiSystemInstruction{
			Parts: []geminiPart{{Text: req.SystemPrompt}},
		}
	}
	if req.Temperature != 0 || req.MaxTokens != 0 {
		body.GenerationConfig = &geminiGenerationConfig{
			Temperature:     req.Temperature,
			MaxOutputTokens: req.MaxTokens,
		}
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return CompletionResponse{}, fmt.Errorf("gemini: marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/%s:generateContent", p.baseURL, p.model)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return CompletionResponse{}, fmt.Errorf("gemini: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-goog-api-key", p.apiKey)

	resp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return CompletionResponse{}, fmt.Errorf("gemini: request failed: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return CompletionResponse{}, fmt.Errorf("gemini: read response: %w", err)
	}

	var parsed geminiResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return CompletionResponse{}, fmt.Errorf("gemini: parse response (status %d): %w, body=%s", resp.StatusCode, err, truncate(raw, 500))
	}

	if resp.StatusCode != http.StatusOK {
		if parsed.Error != nil {
			return CompletionResponse{}, fmt.Errorf("gemini: api error (status %d): %s", resp.StatusCode, parsed.Error.Message)
		}
		return CompletionResponse{}, fmt.Errorf("gemini: unexpected status %d: %s", resp.StatusCode, truncate(raw, 500))
	}

	if len(parsed.Candidates) == 0 {
		return CompletionResponse{}, fmt.Errorf("gemini: empty candidates in response (kemungkinan diblok oleh safety filter): %s", truncate(raw, 500))
	}

	candidate := parsed.Candidates[0]
	if len(candidate.Content.Parts) == 0 {
		return CompletionResponse{}, fmt.Errorf(
			"gemini: model %q returned empty content (finish_reason=%q, candidates_token_count=%d). "+
				"Kemungkinan MaxTokens terlalu kecil atau kena safety filter.",
			p.model, candidate.FinishReason, parsed.UsageMetadata.CandidatesTokenCount,
		)
	}

	var text string
	for _, part := range candidate.Content.Parts {
		text += part.Text
	}

	return CompletionResponse{
		Content:      text,
		InputTokens:  parsed.UsageMetadata.PromptTokenCount,
		OutputTokens: parsed.UsageMetadata.CandidatesTokenCount,
		FinishReason: candidate.FinishReason,
	}, nil
}
