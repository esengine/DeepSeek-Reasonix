package main

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"reasonix/desktop/internal/browserops"
	"reasonix/internal/browser"
	"reasonix/internal/extension/rpcwire"
)

type fakeBrowserHost struct {
	mu      sync.Mutex
	calls   []string
	replies map[string]any
	errs    map[string]error
}

func (f *fakeBrowserHost) Request(_ context.Context, method string, params any, result any) error {
	f.mu.Lock()
	f.calls = append(f.calls, method)
	reply, hasReply := f.replies[method]
	err := f.errs[method]
	f.mu.Unlock()
	if err != nil {
		return err
	}
	if result == nil || !hasReply {
		return nil
	}
	raw, _ := json.Marshal(reply)
	return json.Unmarshal(raw, result)
}

func (f *fakeBrowserHost) methods() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

func newBrowserExecutorForTest(t *testing.T, host *fakeBrowserHost) (*App, *hostBrowserExecutor) {
	t.Helper()
	isolateDesktopUserDirs(t)
	a := NewApp()
	a.hostShell = &hostShellBridge{app: a}
	exec := &hostBrowserExecutor{app: a, host: host, tabID: "tab-1", grantID: "grant-tab-1"}
	return a, exec
}

func TestBrowserExecutorGrantsOnceAndCarriesGrantID(t *testing.T) {
	host := &fakeBrowserHost{replies: map[string]any{"host/browser.tabs.list": map[string]any{"tabs": []map[string]any{{"id": "b1", "url": "https://example.test", "title": "Example"}}}}}
	_, exec := newBrowserExecutorForTest(t, host)
	for range 2 {
		tabs, err := exec.Tabs(context.Background())
		if err != nil || len(tabs) != 1 || tabs[0].ID != "b1" {
			t.Fatalf("tabs: %+v err=%v", tabs, err)
		}
	}
	got := host.methods()
	want := []string{"host/browser.grant", "host/browser.tabs.list", "host/browser.tabs.list"}
	if len(got) != len(want) {
		t.Fatalf("calls %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("calls %v, want %v", got, want)
		}
	}
}

func TestBrowserExecutorActSettlesFromReceipt(t *testing.T) {
	host := &fakeBrowserHost{replies: map[string]any{"host/browser.act": map[string]any{"executed": true, "documentToken": "doc-2"}}}
	a, exec := newBrowserExecutorForTest(t, host)
	req := browser.ActRequest{OperationID: "op-1", TabID: "b1", DocumentToken: "doc-1", Action: browser.ActionClick, Ref: "e3"}
	res, err := exec.Act(context.Background(), req)
	if err != nil || !res.Executed || res.Outcome != browser.OutcomeExecuted || res.DocumentToken != "doc-2" {
		t.Fatalf("act: %+v err=%v", res, err)
	}
	ledger, err := a.browserLedger()
	if err != nil {
		t.Fatal(err)
	}
	op, ok := ledger.Lookup("op-1")
	if !ok || op.State != browserops.StateExecuted || op.TabID != "b1" || op.Digest == "" {
		t.Fatalf("ledger after executed act: %+v ok=%v", op, ok)
	}

	res, err = exec.Act(context.Background(), req)
	if !errors.Is(err, browser.ErrUnknownOutcome) || res.Executed || res.Outcome != browser.OutcomeUnknown {
		t.Fatalf("duplicate operation must not reach the shell: %+v err=%v", res, err)
	}
	if calls := host.methods(); len(calls) != 2 {
		t.Fatalf("shell calls %v, want grant + one act", calls)
	}
}

func TestBrowserExecutorActNotExecutedAndStale(t *testing.T) {
	host := &fakeBrowserHost{replies: map[string]any{"host/browser.act": map[string]any{"executed": false, "reason": "element missing"}}}
	a, exec := newBrowserExecutorForTest(t, host)
	res, err := exec.Act(context.Background(), browser.ActRequest{OperationID: "op-a", TabID: "b1", DocumentToken: "d", Action: browser.ActionClick, Ref: "e1"})
	if err != nil || res.Executed || res.Reason != "element missing" {
		t.Fatalf("not executed: %+v err=%v", res, err)
	}
	host.errs = map[string]error{"host/browser.act": &rpcwire.ResponseError{Code: hostBrowserErrStaleReference, Message: "stale"}}
	_, err = exec.Act(context.Background(), browser.ActRequest{OperationID: "op-b", TabID: "b1", DocumentToken: "d", Action: browser.ActionClick, Ref: "e1"})
	if !errors.Is(err, browser.ErrStaleReference) {
		t.Fatalf("stale reference error: %v", err)
	}
	ledger, _ := a.browserLedger()
	for _, id := range []string{"op-a", "op-b"} {
		if op, _ := ledger.Lookup(id); op.State != browserops.StateNotExecuted {
			t.Fatalf("%s state %q, want not_executed", id, op.State)
		}
	}
}

func TestBrowserExecutorLostReceiptIsUnknown(t *testing.T) {
	host := &fakeBrowserHost{errs: map[string]error{"host/browser.act": errors.New("connection closed")}}
	a, exec := newBrowserExecutorForTest(t, host)
	res, err := exec.Act(context.Background(), browser.ActRequest{OperationID: "op-x", TabID: "b1", DocumentToken: "d", Action: browser.ActionType, Ref: "e1", Text: "hi"})
	if !errors.Is(err, browser.ErrUnknownOutcome) || res.Outcome != browser.OutcomeUnknown {
		t.Fatalf("lost receipt: %+v err=%v", res, err)
	}
	ledger, _ := a.browserLedger()
	unknown := ledger.Unsettled()
	if len(unknown) != 1 || unknown[0].ID != "op-x" {
		t.Fatalf("unsettled: %+v", unknown)
	}
	if _, err := exec.Act(context.Background(), browser.ActRequest{OperationID: "op-x", TabID: "b1", DocumentToken: "d", Action: browser.ActionType, Ref: "e1", Text: "hi"}); !errors.Is(err, browser.ErrUnknownOutcome) {
		t.Fatalf("a repeated unknown id must preserve the no-retry outcome: %v", err)
	}
	if calls := host.methods(); len(calls) != 2 {
		t.Fatalf("an unknown operation must never be replayed: %v", calls)
	}
}

func TestBrowserExecutorPartialReceiptPersistsUnknown(t *testing.T) {
	host := &fakeBrowserHost{replies: map[string]any{"host/browser.act": map[string]any{"executed": false, "outcome": "unknown", "reason": "takeover after focus click"}}}
	a, exec := newBrowserExecutorForTest(t, host)
	res, err := exec.Act(context.Background(), browser.ActRequest{OperationID: "partial", TabID: "b1", DocumentToken: "d", Action: browser.ActionType})
	if !errors.Is(err, browser.ErrUnknownOutcome) || res.Outcome != browser.OutcomeUnknown {
		t.Fatalf("partial receipt: %+v %v", res, err)
	}
	ledger, _ := a.browserLedger()
	if op, _ := ledger.Lookup("partial"); op.State != browserops.StateUnknown {
		t.Fatalf("partial action settled as %s", op.State)
	}
}

func TestBrowserExecutorMissingReceiptCannotMeanNotExecuted(t *testing.T) {
	host := &fakeBrowserHost{replies: map[string]any{"host/browser.act": map[string]any{}}}
	a, exec := newBrowserExecutorForTest(t, host)
	_, err := exec.Act(context.Background(), browser.ActRequest{OperationID: "missing", TabID: "b1", Action: browser.ActionClick})
	if !errors.Is(err, browser.ErrUnknownOutcome) {
		t.Fatalf("missing receipt: %v", err)
	}
	ledger, _ := a.browserLedger()
	if op, _ := ledger.Lookup("missing"); op.State != browserops.StateUnknown {
		t.Fatalf("missing receipt settled as %s", op.State)
	}
}

func TestBrowserExecutorEveryTabWriteReservesBeforeDispatch(t *testing.T) {
	for _, action := range []string{"open", "navigate", "close"} {
		t.Run(action, func(t *testing.T) {
			host := &fakeBrowserHost{errs: map[string]error{"host/browser.tabs." + action: errors.New("receipt lost")}}
			a, exec := newBrowserExecutorForTest(t, host)
			call := func() error {
				switch action {
				case "open":
					_, err := exec.Open(context.Background(), browser.OpenRequest{OperationID: "op", URL: "https://example.test"})
					return err
				case "navigate":
					_, err := exec.Navigate(context.Background(), browser.NavigateRequest{OperationID: "op", TabID: "b1", Action: browser.NavigateBack})
					return err
				default:
					return exec.Close(context.Background(), browser.CloseRequest{OperationID: "op", TabID: "b1"})
				}
			}
			for range 2 {
				if err := call(); !errors.Is(err, browser.ErrUnknownOutcome) {
					t.Fatalf("lost/duplicate receipt: %v", err)
				}
			}
			if len(host.methods()) != 2 {
				t.Fatalf("write replayed: %v", host.methods())
			}
			ledger, _ := a.browserLedger()
			if op, _ := ledger.Lookup("op"); op.State != browserops.StateUnknown || op.Action != action {
				t.Fatalf("ledger: %+v", op)
			}
		})
	}
}

func TestBrowserExecutorRevokedFailsClosed(t *testing.T) {
	host := &fakeBrowserHost{}
	a, exec := newBrowserExecutorForTest(t, host)
	a.browserExecutors = map[string]*hostBrowserExecutor{"tab-1": exec}
	a.forgetBrowserExecutorLocked("tab-1")
	if exec.Available(context.Background()) {
		t.Fatal("revoked executor must report unavailable")
	}
	if _, err := exec.Tabs(context.Background()); !errors.Is(err, browser.ErrNoGrant) {
		t.Fatalf("revoked executor call: %v", err)
	}
	if a.browserExecutorForTab(&WorkspaceTab{ID: "tab-2"}) == nil {
		t.Fatal("host mode must hand out executors")
	}
	a.hostShell = nil
	if a.browserExecutorForTab(&WorkspaceTab{ID: "tab-3"}) != nil {
		t.Fatal("without the shell no executor may be registered")
	}
}
