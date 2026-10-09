package plugin

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRevisionAuxiliaryWarningMasksStatusBody(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     *int   `json:"id"`
			Method string `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
			return
		}
		switch req.Method {
		case "initialize":
			writeHTTPRPCResult(w, req.ID, map[string]any{"protocolVersion": "2025-06-18", "serverInfo": map[string]any{"name": "neutral", "version": "0"}, "capabilities": map[string]any{"tools": map[string]any{}, "prompts": map[string]any{}}})
		case "tools/list":
			writeHTTPRPCResult(w, req.ID, map[string]any{"tools": []any{}})
		case "prompts/list":
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("Bearer rxprobe https://host" + r.URL.String()))
		default:
			writeHTTPRPCResult(w, req.ID, map[string]any{})
		}
	}))
	defer srv.Close()
	host, _, err := StartAll(t.Context(), []Spec{{Name: "neutral", Type: "http", URL: srv.URL + "/mcp?%74oken=fixturesecret"}})
	if err != nil {
		t.Fatal(err)
	}
	defer host.Close()
	host.fetchPrompts(t.Context(), host.clients[0], nil)
	if logs.Len() == 0 || strings.Contains(logs.String(), "fixturesecret") || strings.Contains(logs.String(), "rxprobe") {
		t.Fatalf("warning leaked: %s", logs.String())
	}
}

func TestRevisionHTTPStatusErrorMasksEndpoint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("https://host" + r.URL.String()))
	}))
	defer srv.Close()
	transport, err := newHTTPTransport(Spec{Name: "neutral", URL: srv.URL + "/mcp?%74oken=fixturesecret"})
	if err != nil {
		t.Fatal(err)
	}
	defer transport.close()
	_, err = transport.call(t.Context(), "prompts/list", nil)
	var status *httpStatusError
	if !errors.As(err, &status) || status.Status != 500 {
		t.Fatalf("status identity lost: %v", err)
	}
	if strings.Contains(err.Error(), "fixturesecret") {
		t.Fatalf("HTTP status leaked: %v", err)
	}
}
