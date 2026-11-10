package feedback

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
)

type withdrawStub struct {
	*stub
	mu     sync.Mutex
	hits   []replyHit
	answer func(w http.ResponseWriter)
}

func (s *withdrawStub) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/feedback/{receipt}/withdraw", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.hits = append(s.hits, replyHit{path: r.URL.Path, id: r.Header.Get("X-Install-Id"), token: r.Header.Get("X-Install-Token")})
		s.mu.Unlock()
		if s.answer != nil {
			s.answer(w)
			return
		}
		_, _ = io.WriteString(w, `{"receipt":"`+r.PathValue("receipt")+`","status":"withdrawn"}`)
	})
	mux.Handle("/", s.stub.handler())
	return mux
}

func withdrawSetup(t *testing.T) (*Service, *withdrawStub) {
	t.Helper()
	ws := &withdrawStub{stub: &stub{}}
	srv := httptest.NewServer(ws.handler())
	t.Cleanup(srv.Close)
	svc, err := New(Config{Home: testenv.TempDir(t), Base: srv.URL, HTTP: srv.Client(), Backoff: []time.Duration{}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Submit(context.Background(), draft()); err != nil {
		t.Fatal(err)
	}
	return svc, ws
}

func TestWithdrawSendsTheInstallIdentityAndRemembersIt(t *testing.T) {
	svc, ws := withdrawSetup(t)
	if err := svc.Withdraw(context.Background(), "FB-7K3M-9QX2"); err != nil {
		t.Fatalf("withdraw: %v", err)
	}
	h := ws.hits[0]
	if h.path != "/v1/feedback/FB-7K3M-9QX2/withdraw" || h.id != ws.posts[0].InstallID || h.token != stubToken(h.id) {
		t.Fatalf("hit = %+v", h)
	}
	it, _ := svc.Item("FB-7K3M-9QX2")
	if it.Status != StatusWithdrawn {
		t.Fatalf("remembered status = %q, want withdrawn", it.Status)
	}
}

func TestWithdrawRefusesBeforeSending(t *testing.T) {
	svc, ws := withdrawSetup(t)
	if err := svc.Withdraw(context.Background(), "not a receipt!"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("a malformed receipt = %v, want ErrInvalid", err)
	}
	if err := svc.Withdraw(context.Background(), "FB-AAAA-BBBB"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("a receipt this machine never filed = %v, want ErrInvalid", err)
	}
	if len(ws.hits) != 0 {
		t.Fatalf("%d requests were sent", len(ws.hits))
	}
}

func TestWithdrawRefusalsKeepTheirIdentity(t *testing.T) {
	for _, tc := range []struct {
		status int
		code   string
		want   error
	}{
		{http.StatusConflict, "feedback.not_withdrawable", ErrNotWithdrawable},
		{http.StatusNotFound, "feedback.not_found", ErrNotFound},
		{http.StatusUnauthorized, "feedback.bad_token", ErrBadToken},
	} {
		svc, ws := withdrawSetup(t)
		ws.answer = func(w http.ResponseWriter) { stubRefuse(w, tc.status, tc.code) }
		err := svc.Withdraw(context.Background(), "FB-7K3M-9QX2")
		if !errors.Is(err, tc.want) {
			t.Fatalf("%s = %v, want %v", tc.code, err, tc.want)
		}
		if it, _ := svc.Item("FB-7K3M-9QX2"); it.Status == StatusWithdrawn && !errors.Is(tc.want, nil) {
			t.Fatalf("%s: a refused withdrawal was remembered as done", tc.code)
		}
	}
}

func TestAReportFromARetiredIdentityCannotBeWithdrawn(t *testing.T) {
	svc, ws := withdrawSetup(t)
	ws.answer = func(w http.ResponseWriter) { stubRefuse(w, http.StatusUnauthorized, "feedback.bad_token") }
	_ = svc.Withdraw(context.Background(), "FB-7K3M-9QX2")
	n := len(ws.hits)
	if err := svc.Withdraw(context.Background(), "FB-7K3M-9QX2"); !errors.Is(err, ErrNotWithdrawable) {
		t.Fatalf("after the identity was retired = %v, want ErrNotWithdrawable", err)
	}
	if len(ws.hits) != n {
		t.Fatal("a request was sent under a retired identity")
	}
}
