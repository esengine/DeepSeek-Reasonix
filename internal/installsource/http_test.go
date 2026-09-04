package installsource

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFetchTextAllowsBodyAtLimit(t *testing.T) {
	body := strings.Repeat("x", defaultFetchLimit)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	tool := NewTool(Options{
		ProjectRoot: t.TempDir(),
		HomeDir:     t.TempDir(),
		HTTPClient:  srv.Client(),
	}).(*installSourceTool)

	got, err := tool.fetchText(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("fetchText() error = %v", err)
	}
	if got != body {
		t.Fatalf("fetchText() returned %d bytes, want %d", len(got), len(body))
	}
}

func TestFetchTextRejectsOversizedBody(t *testing.T) {
	body := strings.Repeat("x", defaultFetchLimit+1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	tool := NewTool(Options{
		ProjectRoot: t.TempDir(),
		HomeDir:     t.TempDir(),
		HTTPClient:  srv.Client(),
	}).(*installSourceTool)

	got, err := tool.fetchText(context.Background(), srv.URL)
	if !errors.Is(err, ErrSourceUnreadable) {
		t.Fatalf("fetchText() error = %v, want ErrSourceUnreadable", err)
	}
	if got != "" {
		t.Fatalf("fetchText() returned %d bytes after rejecting oversized body", len(got))
	}
}
