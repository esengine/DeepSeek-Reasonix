package cli

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/control"
	"reasonix/internal/event"
)

type takeoverReturnServers struct {
	old, new       *httptest.Server
	mu             sync.Mutex
	oldEnd, newEnd []string
}

func (s *takeoverReturnServers) recordEnd(t *testing.T, r *http.Request, old bool) {
	var body struct {
		MirrorID string `json:"mirrorId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		t.Error(err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if old {
		s.oldEnd = append(s.oldEnd, body.MirrorID)
	} else {
		s.newEnd = append(s.newEnd, body.MirrorID)
	}
}

func newTakeoverReturnServers(t *testing.T, path string) *takeoverReturnServers {
	t.Helper()
	s := &takeoverReturnServers{}
	s.new = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/auth/token":
			w.WriteHeader(http.StatusNoContent)
		case "/adopt":
			_ = json.NewEncoder(w).Encode(cliTakeoverGrant{SessionPath: path, MirrorID: "mirror-new", ReturnHandoffID: "return-new", SourceWriterID: "serve-new", TargetWriterID: agent.SessionWriterID()})
		case "/mirror-end":
			s.recordEnd(t, r, false)
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	s.old = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/mirror-end" {
			s.recordEnd(t, r, true)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	previous := discoverCLIServesForTakeover
	discoverCLIServesForTakeover = func() []cliServeRecord { return []cliServeRecord{{base: s.new.URL, token: "fresh"}} }
	t.Cleanup(func() { discoverCLIServesForTakeover = previous; s.old.Close(); s.new.Close() })
	return s
}

type takeoverReturnEnv struct {
	source, target string
	servers        *takeoverReturnServers
	leases         *control.SessionLeaseKeeper
	manager        *cliTakeoverManager
	oldBinding     *cliTakeoverBinding
}

func newTakeoverReturnEnv(t *testing.T) *takeoverReturnEnv {
	t.Helper()
	dir := t.TempDir()
	e := &takeoverReturnEnv{source: filepath.Join(dir, "source.jsonl"), target: filepath.Join(dir, "target.jsonl")}
	e.servers = newTakeoverReturnServers(t, e.source)
	e.leases = control.NewSessionLeaseKeeper()
	t.Cleanup(e.leases.Release)
	if err := e.leases.Rebind(e.source); err != nil {
		t.Fatal(err)
	}
	e.oldBinding = &cliTakeoverBinding{path: e.source, record: cliServeRecord{base: e.servers.old.URL}, client: e.servers.old.Client(), grant: cliTakeoverGrant{MirrorID: "mirror-old", SourceWriterID: "serve-old", ReturnHandoffID: "return-old"}}
	e.manager = newCLITakeoverManager(&takeoverRecordSink{}, e.leases)
	e.manager.binding, e.manager.revision = e.oldBinding, 1
	e.manager.Emit(event.Event{Kind: event.Text, Text: "flush before return"})
	return e
}

func (e *takeoverReturnEnv) assertReturned(t *testing.T) {
	t.Helper()
	if got := e.leases.HeldPath(); got != agent.CanonicalSessionPath(e.target) {
		t.Fatalf("held path = %q, want target", got)
	}
	info, err := agent.LoadSessionLeaseInfo(e.source)
	if err != nil || info == nil || info.HandoffTo != "serve-new" || info.HandoffID != "return-new" {
		t.Fatalf("source reservation = %+v, error=%v", info, err)
	}
	e.servers.mu.Lock()
	defer e.servers.mu.Unlock()
	if len(e.servers.oldEnd) != 0 || len(e.servers.newEnd) != 1 || e.servers.newEnd[0] != "mirror-new" {
		t.Fatalf("mirror-end old=%v new=%v", e.servers.oldEnd, e.servers.newEnd)
	}
}

func (e *takeoverReturnEnv) assertActiveLeaseReturned(t *testing.T) {
	t.Helper()
	if got := e.leases.HeldPath(); got != "" {
		t.Fatalf("held path after return = %q, want none", got)
	}
	info, err := agent.LoadSessionLeaseInfo(e.source)
	if err != nil || info == nil || info.HandoffTo != "serve-new" || info.HandoffID != "return-new" {
		t.Fatalf("source reservation = %+v, error=%v", info, err)
	}
	binding, _, _, _ := e.manager.snapshot()
	if binding != nil {
		t.Fatalf("active binding survived return: %+v", binding.grant)
	}
	e.servers.mu.Lock()
	defer e.servers.mu.Unlock()
	if len(e.servers.oldEnd) != 0 || len(e.servers.newEnd) != 1 || e.servers.newEnd[0] != "mirror-new" {
		t.Fatalf("mirror-end old=%v new=%v", e.servers.oldEnd, e.servers.newEnd)
	}
}

func TestCLITakeoverManagerReturnsRefreshedMirrorGeneration(t *testing.T) {
	for _, tc := range []struct {
		name string
		run  func(*takeoverReturnEnv) error
	}{
		{name: "RebindAway", run: func(e *takeoverReturnEnv) error {
			handled, err := e.manager.RebindAway(e.target)
			if !handled && err == nil {
				return errors.New("RebindAway did not handle the mirror")
			}
			return err
		}},
		{name: "commitPriorMirror", run: func(e *takeoverReturnEnv) error {
			previous, err := e.leases.RebindDetaching(e.target)
			if err != nil {
				return err
			}
			next := &cliTakeoverBinding{path: e.target, previous: previous, priorMirror: e.oldBinding}
			if err := next.commitPrevious(e.manager); err != nil {
				return err
			}
			if next.previous != nil {
				return errors.New("detached source keeper retained")
			}
			return nil
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newTakeoverReturnEnv(t)
			if err := tc.run(e); err != nil {
				t.Fatal(err)
			}
			e.assertReturned(t)
		})
	}
}

func TestCLITakeoverManagerReturnFailureKeepsRefreshedMirrorActive(t *testing.T) {
	e := newTakeoverReturnEnv(t)
	wantErr := errors.New("injected reverse reservation failure")
	err := e.manager.returnCurrentMirror(e.source, func(current *cliTakeoverBinding) error {
		if current.grant.MirrorID != "mirror-new" || current.grant.SourceWriterID != "serve-new" || current.grant.ReturnHandoffID != "return-new" {
			t.Fatalf("return binding = %+v", current.grant)
		}
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("return error = %v", err)
	}
	binding, _, _, revision := e.manager.snapshot()
	e.manager.mu.Lock()
	queued := e.manager.queue.Len()
	e.manager.mu.Unlock()
	if binding == nil || binding.grant.MirrorID != "mirror-new" || revision <= 1 || e.manager.Returned() || queued != 1 {
		t.Fatalf("binding=%+v revision=%d returned=%v queued=%d", binding, revision, e.manager.Returned(), queued)
	}
	if got := e.leases.HeldPath(); got != agent.CanonicalSessionPath(e.source) {
		t.Fatalf("source lease moved to %q", got)
	}
	e.servers.mu.Lock()
	defer e.servers.mu.Unlock()
	if len(e.servers.oldEnd)+len(e.servers.newEnd) != 0 {
		t.Fatalf("mirror ended after reservation failure")
	}
}

func TestCLITakeoverManagerReclaimAndCloseReturnRefreshedGeneration(t *testing.T) {
	for _, tc := range []struct {
		name string
		run  func(*takeoverReturnEnv) error
	}{
		{name: "reclaim", run: func(e *takeoverReturnEnv) error {
			e.manager.reclaiming.Store(true)
			return e.manager.returnLeaseFor(e.oldBinding, 1)
		}},
		{name: "close", run: func(e *takeoverReturnEnv) error {
			return e.manager.Close()
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newTakeoverReturnEnv(t)
			if err := tc.run(e); err != nil {
				t.Fatal(err)
			}
			e.assertActiveLeaseReturned(t)
		})
	}
}
