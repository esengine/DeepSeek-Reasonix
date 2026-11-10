package feedback

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
)

type linkStub struct {
	mu   sync.Mutex
	seen []*http.Request
	code int
	body string
}

func (l *linkStub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	l.mu.Lock()
	l.seen = append(l.seen, r.Clone(context.Background()))
	code, body := l.code, l.body
	l.mu.Unlock()
	if code == 0 {
		code = 200
	}
	w.WriteHeader(code)
	_, _ = io.WriteString(w, body)
}

func linkService(t *testing.T, h http.Handler, withInstall bool, assertion ...string) *Service {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	cfg := Config{Home: testenv.TempDir(t), Base: srv.URL, Backoff: []time.Duration{}}
	if len(assertion) > 0 {
		cfg.Assert = func(context.Context) (string, error) {
			if assertion[0] == "" {
				return "", ErrAccountRequired
			}
			return assertion[0], nil
		}
	}
	s, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if withInstall {
		if err := s.store.update(func(st *state) error { st.InstallID, st.InstallToken = "install-abcdefghij", "tok"; return nil }); err != nil {
			t.Fatal(err)
		}
	}
	return s
}

func TestLinkNeedsAnAssertionAndAnInstallBeforeAnythingLeaves(t *testing.T) {
	stub := &linkStub{}
	s := linkService(t, stub, false, "v1.a.b")
	if err := s.Link(context.Background()); !errors.Is(err, ErrNoInstall) {
		t.Fatalf("install that never reported: %v", err)
	}
	if err := s.Unlink(context.Background()); !errors.Is(err, ErrNoInstall) {
		t.Fatalf("unlink without install: %v", err)
	}
	withInstall := linkService(t, stub, true, "")
	if err := withInstall.Link(context.Background()); !errors.Is(err, ErrAccountRequired) {
		t.Fatalf("no assertion: %v", err)
	}
	if len(stub.seen) != 0 {
		t.Fatalf("%d requests left the machine", len(stub.seen))
	}
}

func TestLinkSendsTheAssertionAndTheInstallProofOnly(t *testing.T) {
	stub := &linkStub{body: `{"linked":true}`}
	s := linkService(t, stub, true, "v1.a.b")
	if err := s.Link(context.Background()); err != nil {
		t.Fatal(err)
	}
	r := stub.seen[0]
	if r.Method != http.MethodPost || r.URL.Path != "/v1/feedback/link" {
		t.Fatalf("request %s %s", r.Method, r.URL.Path)
	}
	if r.Header.Get(assertionHeader) != "v1.a.b" || r.Header.Get("X-Install-Id") != "install-abcdefghij" || r.Header.Get("X-Install-Token") != "tok" {
		t.Fatalf("headers %v", r.Header)
	}
}

func TestUnlinkByShortIdSendsNoInstallProof(t *testing.T) {
	stub := &linkStub{body: `{"ok":true}`}
	s := linkService(t, stub, true, "v1.a.b")
	if err := s.UnlinkInstall(context.Background(), "0123456789ab"); err != nil {
		t.Fatal(err)
	}
	r := stub.seen[0]
	if r.Method != http.MethodDelete || r.URL.Path != "/v1/feedback/link/0123456789ab" {
		t.Fatalf("request %s %s", r.Method, r.URL.Path)
	}
	if r.Header.Get("X-Install-Token") != "" {
		t.Fatal("the account is the authority; no install token should be sent")
	}
	for _, id := range []string{"short", "0123456789AB", "../../x12345", "0123456789zz"} {
		var bad *InvalidError
		if err := s.UnlinkInstall(context.Background(), id); !errors.As(err, &bad) || bad.Field != FieldInstall {
			t.Fatalf("malformed short id %q: %v", id, err)
		}
	}
}

func TestLinksListsInstallsAndWorksWithoutAnInstall(t *testing.T) {
	stub := &linkStub{body: `{"installs":[{"id":"0123456789ab","linkedAt":"2026-10-15T00:00:00Z","current":true},{"id":"ba9876543210","linkedAt":"2026-10-14T00:00:00Z","current":false}]}`}
	s := linkService(t, stub, false, "v1.a.b")
	got, err := s.Links(context.Background())
	if err != nil || len(got) != 2 || !got[0].Current || got[1].Current {
		t.Fatalf("links %v %v", got, err)
	}
	if stub.seen[0].Header.Get("X-Install-Token") != "" {
		t.Fatal("no install, so no proof")
	}
}

func TestLinkRefusalsKeepTheirIdentity(t *testing.T) {
	for code, want := range map[string]error{
		"feedback.account_required": ErrAccountRequired,
		"feedback.link_conflict":    ErrLinkConflict,
		"feedback.link_limit":       ErrLinkLimit,
	} {
		stub := &linkStub{code: 403, body: `{"error":{"code":"` + code + `","message":"x"}}`}
		s := linkService(t, stub, true, "v1.a.b")
		if err := s.Link(context.Background()); !errors.Is(err, want) {
			t.Fatalf("%s: got %v", code, err)
		}
	}
}

func TestAServiceWithoutASignInHasNoAccountToLinkTo(t *testing.T) {
	stub := &linkStub{}
	s := linkService(t, stub, true)
	for _, err := range []error{s.Link(context.Background()), s.Unlink(context.Background()), s.UnlinkInstall(context.Background(), "0123456789ab")} {
		if !errors.Is(err, ErrAccountRequired) {
			t.Fatalf("got %v", err)
		}
	}
	if _, err := s.Links(context.Background()); !errors.Is(err, ErrAccountRequired) {
		t.Fatalf("links: %v", err)
	}
	if len(stub.seen) != 0 {
		t.Fatal("nothing should leave without an account")
	}
}

func TestTheInstallTokenNeverAppearsInAnyReturnedValue(t *testing.T) {
	stub := &linkStub{body: `{"installs":[{"id":"0123456789ab","linkedAt":"2026-10-15T00:00:00Z","current":true}]}`}
	s := linkService(t, stub, true, "v1.a.b")
	got, err := s.Links(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprintf("%+v", got) == "" || strings.Contains(fmt.Sprintf("%+v", got), "tok") {
		t.Fatalf("link list leaks the proof: %+v", got)
	}
}
