package responses

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"reasonix/internal/contract/provider"
)

func TestResponsesIdentifyReasonix(t *testing.T) {
	provider.SetClientVersion("9.8.7")
	t.Cleanup(func() { provider.SetClientVersion("") })
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("User-Agent")
		writeEvents(w, `{"type":"response.completed","response":{"id":"resp_1","usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}`)
	}))
	defer srv.Close()

	p := New(Config{Name: "relay", BaseURL: srv.URL + "/v1", Model: "m", APIKey: "k"})
	ch, err := p.Stream(context.Background(), provider.Request{Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}}})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	for chunk := range ch {
		if chunk.Type == provider.ChunkError {
			t.Fatalf("stream error: %v", chunk.Err)
		}
	}
	if got != "Reasonix/9.8.7" {
		t.Fatalf("User-Agent = %q, want Reasonix/9.8.7", got)
	}
}
