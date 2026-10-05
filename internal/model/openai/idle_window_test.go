package openai

import (
	"net/http"
	"testing"
	"time"

	"reasonix/internal/contract/provider"
)

// A local model prefilling a long context sends nothing for minutes; two
// minutes of silence cut those turns off as dropped connections (#6990).
func TestNewWaitsFiveMinutesForASilentModel(t *testing.T) {
	p, err := New(provider.Config{Name: "local", BaseURL: "http://127.0.0.1:8080/v1", Model: "qwen", APIKey: "k"})
	if err != nil {
		t.Fatal(err)
	}
	c := p.(*client)
	if c.idleTimeout < 5*time.Minute {
		t.Fatalf("stream idle window = %s, want at least 5m", c.idleTimeout)
	}
	tr, ok := c.http.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport = %T, want *http.Transport", c.http.Transport)
	}
	if tr.ResponseHeaderTimeout < 5*time.Minute {
		t.Fatalf("response header window = %s, want at least 5m", tr.ResponseHeaderTimeout)
	}
}
