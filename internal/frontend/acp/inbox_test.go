package acp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/session/control"
	"reasonix/internal/state/sessioninbox"
)

// requireRefusal holds one refusal to the pair the docs promise a client: the
// JSON-RPC code for its class, and the identity it branches on. The message
// stays: clients still show it.
func requireRefusal(t *testing.T, f frame, wantCode int, wantIdentity string) {
	t.Helper()
	if f.Error == nil {
		t.Fatalf("response %s, want a refusal", f.Result)
	}
	var data struct {
		Code string `json:"code"`
	}
	if len(f.Error.Data) > 0 {
		if err := json.Unmarshal(f.Error.Data, &data); err != nil {
			t.Fatalf("refusal data %s: %v", f.Error.Data, err)
		}
	}
	if f.Error.Code != wantCode || data.Code != wantIdentity {
		t.Fatalf("refusal = %s, want code %d and data.code %q", wireError(f), wantCode, wantIdentity)
	}
	if f.Error.Message == "" {
		t.Fatalf("refusal carries no message; clients still show it: %s", wireError(f))
	}
}

// wireError renders the error member as the client received it, so a failing
// expectation shows the payload a client had to branch on.
func wireError(f frame) string {
	raw, err := json.Marshal(f.Error)
	if err != nil {
		return "<unprintable>"
	}
	return string(raw)
}

// TestEveryInboxRefusalCarriesItsDocumentedCode holds each condition the queue
// declares to the pair docs/ACP.md promises. repolint holds the switch to the
// store's family; this holds every case to the identity it writes.
func TestEveryInboxRefusalCarriesItsDocumentedCode(t *testing.T) {
	cases := []struct {
		err      error
		code     int
		identity string
	}{
		{control.ErrSteerApplied, ErrInboxRefused, "steer.already_applied"},
		{sessioninbox.ErrItemTooLarge, ErrInboxRefused, "inbox.item_too_large"},
		{sessioninbox.ErrCapacityItems, ErrInboxRefused, "inbox.capacity_items"},
		{sessioninbox.ErrCapacityBytes, ErrInboxRefused, "inbox.capacity_bytes"},
		{sessioninbox.ErrInvalidState, ErrInboxRefused, "inbox.invalid_state"},
		{sessioninbox.ErrRetryMayRepeat, ErrInboxMayHaveRun, "inbox.retry_may_repeat"},
		{sessioninbox.ErrForeignRun, ErrInboxRefused, "inbox.foreign_run"},
		{sessioninbox.ErrPaused, ErrInboxRefused, "inbox.paused"},
		{sessioninbox.ErrUnrecovered, ErrInboxRefused, "inbox.unrecovered"},
		{sessioninbox.ErrNotFound, ErrInboxRefused, "inbox.not_found"},
		{sessioninbox.ErrIdempotencyConflict, ErrInboxRefused, "inbox.idempotency_conflict"},
		{sessioninbox.ErrSchemaReadonly, ErrInboxRefused, "inbox.schema_readonly"},
		{sessioninbox.ErrClosed, ErrInboxRefused, "inbox.closed"},
		{sessioninbox.ErrEmpty, ErrInvalidParams, "inbox.empty"},
	}
	seen := map[string]error{}
	for _, tc := range cases {
		// Wrapped, because that is how these arrive: the store or the controller
		// adds which item it was about, and a switch reading the sentence
		// instead of the sentinel stops matching right there.
		got := inboxRefusal(fmt.Errorf("inbox item q1: %w", tc.err))
		data, ok := got.Data.(InboxRefusalData)
		if !ok || data.Code != tc.identity || got.Code != tc.code {
			t.Errorf("%v: refusal = %d/%q, want %d/%q", tc.err, got.Code, data.Code, tc.code, tc.identity)
		}
		if got.Message == "" {
			t.Errorf("%v: refusal carries no message; clients still show it", tc.err)
		}
		if prev, dup := seen[data.Code]; dup {
			t.Errorf("%q names both %v and %v; two conditions under one identity cannot be told apart", data.Code, prev, tc.err)
		}
		seen[data.Code] = tc.err
	}
}

// A failure this surface cannot name is not given an identity: a code would say
// the queue refused, when what happened is that it could not be read.
func TestAnUnnamedInboxFailureIsAFaultNotARefusal(t *testing.T) {
	got := inboxRefusal(errors.New("disk went away"))
	if got.Code != ErrInternal {
		t.Errorf("code %d, want %d", got.Code, ErrInternal)
	}
	if got.Data != nil {
		t.Errorf("data = %+v, want none", got.Data)
	}
}

// TestServeInboxRetryOnAMissingItemNamesTheRefusal is the probe that motivated
// the contract: the same answer -32600 as a malformed frame, no data.code, so a
// client could not tell "gone" from "your request was broken" — and one that
// retried on any error re-sent work the queue had already refused.
func TestServeInboxRetryOnAMissingItemNamesTheRefusal(t *testing.T) {
	factory := &configurableFactory{dir: testenv.TempDir(t)}
	client, stop := startServer(t, factory)
	defer stop()

	sid := openInboxSession(t, client)
	resp := client.call(t, sessionInboxRetryMethod, SessionInboxItemParams{SessionID: sid, ItemID: "no-such-item"})
	requireRefusal(t, resp, ErrInboxRefused, "inbox.not_found")
}

// TestServeInboxRetryOnWorkThatMayHaveRunCarriesThatIdentity is the refusal a
// client must never retry blindly: the item's block code is not steer_unapplied,
// so the turn may already have run. It answers with its own JSON-RPC code, so a
// retry policy that knows only the classes still stops.
func TestServeInboxRetryOnWorkThatMayHaveRunCarriesThatIdentity(t *testing.T) {
	factory := &configurableFactory{dir: testenv.TempDir(t)}
	// The hook hands the test the live controller, which is how it reaches the
	// queue on disk to put one item in the state a crashed run leaves behind.
	captured := make(chan *control.Controller, 1)
	factory.withCtrl = func(_ context.Context, sink event.Sink, _ string, _ SessionParams, ctrl *control.Controller) error {
		captured <- ctrl
		sink.Emit(event.Event{Kind: event.Text, Text: "done"})
		return nil
	}
	client, stop := startServer(t, factory)
	defer stop()

	sid := openInboxSession(t, client)
	promptCh := client.callAsync("session/prompt", SessionPromptParams{
		SessionID: sid,
		Prompt:    []ContentBlock{{Type: "text", Text: "first turn"}},
	})
	if _, resp := drainPrompt(t, client, promptCh); resp.Error != nil {
		t.Fatalf("prompt: %+v", resp.Error)
	}
	ctrl := <-captured

	enqueued := client.call(t, sessionInboxEnqueueMethod, SessionInboxEnqueueParams{SessionID: sid, Text: "queued work"})
	if enqueued.Error != nil {
		t.Fatalf("enqueue: %+v", enqueued.Error)
	}
	var rec struct {
		ItemID string `json:"itemId"`
	}
	if err := json.Unmarshal(enqueued.Result, &rec); err != nil || rec.ItemID == "" {
		t.Fatalf("enqueue result %s: %v", enqueued.Result, err)
	}

	// The block code a transcript-snapshot failure leaves: not steer_unapplied,
	// so RetrySafe is false and the queue says the work may already have run.
	st, err := sessioninbox.Open(ctrl.SessionPath(), sessioninbox.Limits{})
	if err != nil {
		t.Fatalf("open queue at %q: %v", ctrl.SessionPath(), err)
	}
	defer st.Close()
	if err := st.SetStateCoded(rec.ItemID, sessioninbox.StateUncertain, sessioninbox.BlockSnapshotFailed, "turn completed but transcript snapshot failed"); err != nil {
		t.Fatalf("set uncertain: %v", err)
	}

	resp := client.call(t, sessionInboxRetryMethod, SessionInboxItemParams{SessionID: sid, ItemID: rec.ItemID})
	requireRefusal(t, resp, ErrInboxMayHaveRun, "inbox.retry_may_repeat")
}

// TestServeInboxSuccessPathIsUnchanged is the control: a call that is not
// refused answers exactly what it answered before, with no error at all.
func TestServeInboxSuccessPathIsUnchanged(t *testing.T) {
	factory := &configurableFactory{dir: testenv.TempDir(t)}
	client, stop := startServer(t, factory)
	defer stop()

	sid := openInboxSession(t, client)
	resp := client.call(t, sessionInboxEnqueueMethod, SessionInboxEnqueueParams{SessionID: sid, Text: "queued work"})
	if resp.Error != nil {
		t.Fatalf("enqueue: %+v", resp.Error)
	}
	var rec struct {
		ItemID      string `json:"itemId"`
		Disposition string `json:"disposition"`
		Position    int    `json:"position"`
	}
	if err := json.Unmarshal(resp.Result, &rec); err != nil {
		t.Fatalf("enqueue result: %v", err)
	}
	if rec.ItemID == "" || rec.Disposition != "queued_followup" || rec.Position != 1 {
		t.Fatalf("enqueue result = %+v, want the first queued follow-up", rec)
	}

	list := client.call(t, sessionInboxListMethod, struct {
		SessionID string `json:"sessionId"`
	}{SessionID: sid})
	if list.Error != nil {
		t.Fatalf("list: %+v", list.Error)
	}
	var snap struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	if err := json.Unmarshal(list.Result, &snap); err != nil {
		t.Fatalf("list result: %v", err)
	}
	if len(snap.Items) != 1 || snap.Items[0].ID != rec.ItemID {
		t.Fatalf("list items = %+v, want the item just enqueued", snap.Items)
	}
}

// TestAMalformedFrameKeepsTheInvalidRequestCode is the other control: -32600
// still exists, and it still means what JSON-RPC says it means.
func TestAMalformedFrameKeepsTheInvalidRequestCode(t *testing.T) {
	factory := &configurableFactory{dir: testenv.TempDir(t)}
	var out bytes.Buffer
	if err := Serve(context.Background(), strings.NewReader(`{"jsonrpc":"2.0"}`+"\n"), &out, factory, AgentInfo{Name: "reasonix-test", Version: "0"}); err != nil {
		t.Fatalf("serve: %v", err)
	}
	var got frame
	if err := json.Unmarshal(bytes.TrimSpace(out.Bytes()), &got); err != nil {
		t.Fatalf("frame %q: %v", out.String(), err)
	}
	if got.Error == nil || got.Error.Code != ErrInvalidRequest {
		t.Fatalf("malformed frame = %s, want code %d", out.String(), ErrInvalidRequest)
	}
}

func openInboxSession(t *testing.T, client *rpcClient) string {
	t.Helper()
	if resp := client.call(t, "initialize", InitializeParams{ProtocolVersion: 1}); resp.Error != nil {
		t.Fatalf("initialize: %+v", resp.Error)
	}
	resp := client.call(t, "session/new", SessionNewParams{Cwd: testenv.TempDir(t)})
	if resp.Error != nil {
		t.Fatalf("session/new: %+v", resp.Error)
	}
	var nr SessionNewResult
	if err := json.Unmarshal(resp.Result, &nr); err != nil || nr.SessionID == "" {
		t.Fatalf("session/new result %s: %v", resp.Result, err)
	}
	return nr.SessionID
}
