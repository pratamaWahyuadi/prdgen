package llm

import "context"

type MockProvider struct {
	Responses []string
	Err       error
	// FinishReason kalau diisi (mis. "length" atau "MAX_TOKENS") akan
	// dikembalikan apa adanya di response -- dipakai untuk mensimulasikan
	// output model yang terpotong. Kosong berarti finish normal.
	FinishReason string

	calls       int
	LastRequest CompletionRequest
}

func (m *MockProvider) Name() string { return "mock" }

func (m *MockProvider) Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error) {
	m.LastRequest = req
	if m.Err != nil {
		return CompletionResponse{}, m.Err
	}
	if len(m.Responses) == 0 {
		return CompletionResponse{Content: "mock response", FinishReason: m.FinishReason}, nil
	}
	idx := m.calls
	if idx >= len(m.Responses) {
		idx = len(m.Responses) - 1
	}
	m.calls++
	return CompletionResponse{Content: m.Responses[idx], FinishReason: m.FinishReason}, nil
}
