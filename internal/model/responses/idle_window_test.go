package responses

import (
	"net/http"
	"testing"
	"time"
)

func TestNewWaitsFiveMinutesForASilentModel(t *testing.T) {
	c := New(Config{Name: "local", BaseURL: "http://127.0.0.1:8080/v1", Model: "qwen", APIKey: "k"}).(*client)
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

func TestSendOptsCarryTheHeaderWindow(t *testing.T) {
	c := New(Config{Name: "local", BaseURL: "http://127.0.0.1:8080/v1", Model: "qwen", APIKey: "k", Extra: map[string]any{"idle_timeout_seconds": 8}}).(*client)
	if got := c.sendOpts().HeaderTimeout; got != 8*time.Second {
		t.Fatalf("HeaderTimeout = %v, want 8s", got)
	}
}
