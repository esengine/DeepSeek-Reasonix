package tui

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestTabCompletesLocalCommandsBeforeAnyKernelReply(t *testing.T) {
	for _, tc := range []struct{ typed, want string }{
		{"/resu", "/resume"},
		{"/ver", "/version"},
		{"/cop", "/copy"},
	} {
		t.Run(tc.typed, func(t *testing.T) {
			m, k := testModel(t)
			typeText(m, tc.typed)
			m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
			if got := m.composer.Value(); got != tc.want {
				t.Fatalf("composer after immediate Tab = %q, want %q", got, tc.want)
			}
			if calledWith(k, "GET /complete") {
				t.Fatal("local completion waited for a kernel request")
			}
		})
	}
}

func TestStaleKernelCompletionKeepsTheCurrentLocalMenu(t *testing.T) {
	m, _ := testModel(t)
	_, old := m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	typeText(m, "resu")
	run(m, old)
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if got := m.composer.Value(); got != "/resume" {
		t.Fatalf("composer after stale reply and Tab = %q, want /resume", got)
	}
}

func TestKernelReplyKeepsTheHighlightedLocalCommand(t *testing.T) {
	m, _ := testModel(t)
	_, pending := m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	press(m, "down")
	run(m, pending)
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if got := m.composer.Value(); got != "/queue" {
		t.Fatalf("composer after Down, reply, Tab = %q, want /queue", got)
	}
}

func TestLocalCompletionSurvivesAKernelFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)
	m := newModel(context.Background(), Options{Client: &Client{HTTP: srv.Client(), Base: srv.URL}})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	typeText(m, "/res")
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'u', Text: "u"})
	run(m, cmd)
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if got := m.composer.Value(); got != "/resume" {
		t.Fatalf("composer after failed lookup and Tab = %q, want /resume", got)
	}
}

func TestTabAfterClearingDoesNotAcceptThePreviousMenu(t *testing.T) {
	m, _ := testModel(t)
	typeText(m, "/resu")
	press(m, "ctrl+c")
	if rows := m.menuLines(); len(rows) != 0 {
		t.Fatalf("the cleared input still shows its old menu: %v", rows)
	}
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if got := m.composer.Value(); got != "" {
		t.Fatalf("Tab restored the cleared command: %q", got)
	}
	if cmd == nil {
		t.Fatal("Tab did not request completions for the cleared input")
	}
}
