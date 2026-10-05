package tui

import (
	tea "charm.land/bubbletea/v2"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"reasonix/internal/base/externalurl"
	"reasonix/internal/base/i18n"
	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/eventwire"
)

func urlAskEvent() eventwire.Event {
	return eventwire.ToWire(event.Event{Kind: event.AskRequest, Ask: event.Ask{ID: "url-ask", Origin: &event.AskOrigin{Kind: "mcp", Source: "external", Message: "Finish\x1b[2J in your browser", URL: "http://127.0.0.1/connect?state=opaque"}, Questions: []event.AskQuestion{{ID: "mcp.url", Prompt: "Complete an external interaction"}}}})
}

func TestURLAnswerCanRetryAfterConnectionFailure(t *testing.T) {
	m, _ := testModel(t)
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) == 1 {
			conn, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			_ = conn.Close()
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)
	m.client = &Client{HTTP: srv.Client(), Base: srv.URL}
	apply(m, urlAskEvent())
	it := m.tr.OpenPrompt()
	m.answerAsk(it, "down")
	m.answerAsk(it, "down")
	cmd, _ := m.answerAsk(it, "enter")
	if m.tr.OpenPrompt() == nil {
		t.Fatal("URL prompt sealed before the answer arrived")
	}
	if duplicate, _ := m.answerAsk(it, "enter"); duplicate != nil {
		t.Fatal("a pending submission allowed a duplicate answer")
	}
	run(m, cmd)
	it = m.tr.OpenPrompt()
	if it == nil || attempts.Load() != 1 {
		t.Fatal("a failed answer did not leave the URL prompt retryable")
	}
	cmd, _ = m.answerAsk(it, "enter")
	run(m, cmd)
	if m.tr.OpenPrompt() != nil || attempts.Load() != 2 || m.tr.Items[0].Verdict != "accept" {
		t.Fatal("retry did not settle the URL prompt exactly once")
	}
}

func TestURLAskWarnsAboutUnicodeAndPunycodeDomains(t *testing.T) {
	for _, target := range []string{"https://bücher.example/connect", "https://xn--bcher-kva.example/connect"} {
		m, _ := testModel(t)
		ev := urlAskEvent()
		ev.Ask.Origin.URL = target
		ev.Ask.Origin.URLHost, ev.Ask.Origin.URLLocal, _ = externalurl.Host(target)
		apply(m, ev)
		if lines := strings.Join(m.askPanel(m.tr.OpenPrompt()), "\n"); !strings.Contains(lines, i18n.M.AskURLWarning) {
			t.Fatalf("internationalized hostname had no warning: %s", target)
		}
	}
}

func TestURLAskUsesDedicatedActionsAndNoTypedAnswer(t *testing.T) {
	for key, action := range map[string]string{"accept": "accept", "decline": "decline", "cancel": "cancel", "esc": "cancel"} {
		t.Run(key, func(t *testing.T) {
			m, k := testModel(t)
			apply(m, urlAskEvent())
			it := m.tr.OpenPrompt()
			lines := strings.Join(m.askPanel(it), "\n")
			if strings.Contains(lines, "\x1b[2J") || !strings.Contains(lines, "http://127.0.0.1/connect?state=opaque") {
				t.Fatalf("unsafe or incomplete URL card: %q", lines)
			}
			if cmd, handled := m.answerAsk(it, "a"); !handled || cmd != nil || m.ask.entering() {
				t.Fatal("URL interaction entered a form editor")
			}
			if key != "esc" {
				steps := map[string]int{"accept": 2, "decline": 3, "cancel": 0}[key]
				for range steps {
					m.answerAsk(it, "down")
				}
				key = "enter"
			}
			cmd, _ := m.answerAsk(it, key)
			run(m, cmd)
			calls := strings.Join(k.seen(), "\n")
			if !strings.Contains(calls, `"Selected":["`+action+`"]`) || strings.Contains(calls, "opaque") {
				t.Fatalf("wrong or unsafe answer: %s", calls)
			}
		})
	}
}

func TestOpeningURLDoesNotAnswerAndFailureKeepsItPending(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX executable fixture")
	}
	dir := testenv.TempDir(t)
	name := "xdg-open"
	if runtime.GOOS == "darwin" {
		name = "open"
	}
	path := filepath.Join(dir, name)
	opened := filepath.Join(dir, "opened")
	t.Setenv("PATH", dir)
	t.Setenv("REASONIX_TEST_OPENED", opened)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n/usr/bin/printf '%s' \"$1\" > \"$REASONIX_TEST_OPENED\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	m, k := testModel(t)
	apply(m, urlAskEvent())
	if _, err := os.Stat(opened); !os.IsNotExist(err) {
		t.Fatal("opened before consent")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	run(m, cmd)
	data, err := os.ReadFile(opened)
	if err != nil || string(data) != "http://127.0.0.1/connect?state=opaque" {
		t.Fatalf("opener=%s,%v", data, err)
	}
	if len(k.seen()) != 0 || m.tr.OpenPrompt() == nil {
		t.Fatal("Open answered the request")
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 1\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	_, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	run(m, cmd)
	if m.tr.OpenPrompt() == nil || len(k.seen()) != 0 {
		t.Fatal("failed opener answered the request")
	}
}

func TestURLAskKeyboardRequiresNavigationForConsent(t *testing.T) {
	m, k := testModel(t)
	apply(m, urlAskEvent())
	for _, key := range []rune{'o', '1', 'y', '2'} {
		_, cmd := m.Update(tea.KeyPressMsg{Code: key, Text: string(key)})
		if cmd != nil {
			t.Fatalf("%c initiated an external interaction", key)
		}
	}
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	run(m, cmd)
	if calls := strings.Join(k.seen(), "\n"); !strings.Contains(calls, `"Selected":["cancel"]`) {
		t.Fatalf("default Enter did not cancel: %s", calls)
	}
}

func TestURLAskShowsNormalizedLocalHost(t *testing.T) {
	m, _ := testModel(t)
	e := urlAskEvent()
	e.Ask.Origin.URL = "https://0x7f.1/connect"
	e.Ask.Origin.URLHost, e.Ask.Origin.URLLocal, _ = externalurl.Host(e.Ask.Origin.URL)
	apply(m, e)
	lines := strings.Join(m.askPanel(m.tr.OpenPrompt()), "\n")
	for _, want := range []string{"127.0.0.1", "https://0x7f.1/connect", i18n.M.AskURLLocalWarning} {
		if !strings.Contains(lines, want) {
			t.Fatalf("missing %q: %s", want, lines)
		}
	}
}
