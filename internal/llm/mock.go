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
	// Requests merekam SEMUA request yang pernah diterima, urut sesuai
	// panggilan -- dipakai test orkestrasi untuk memeriksa isi request
	// di tengah alur (mis. feedback revisi), bukan cuma yang terakhir.
	Requests []CompletionRequest
}

func (m *MockProvider) Name() string { return "mock" }

// CallsCount melaporkan berapa kali Complete dipanggil -- dipakai test
// orkestrasi untuk memastikan jumlah panggilan LLM sesuai alur yang
// diharapkan (mis. validasi + revisi + re-validasi = 3).
func (m *MockProvider) CallsCount() int { return m.calls }

func (m *MockProvider) Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error) {
	m.LastRequest = req
	m.Requests = append(m.Requests, req)
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
