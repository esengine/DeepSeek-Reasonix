package acp

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/provider"
	"reasonix/internal/safety/permission"
	"reasonix/internal/session/control"
	"reasonix/internal/state/sessionstore"
	"reasonix/internal/state/store"
)

func leaseTestFactory(dir string) *e2eFactory {
	return &e2eFactory{
		prov: &scriptedProvider{name: "fake", responses: [][]provider.Chunk{
			{{Type: provider.ChunkText, Text: "unused"}, {Type: provider.ChunkDone}},
		}},
		tool:       fakeTool{name: "peek", ro: true, out: "unused"},
		policy:     permission.New("ask", nil, nil, nil),
		sessionDir: dir,
	}
}

// saveLeaseTestSession writes the transcript an ACP session method will try to
// bind, so the test reaches the lease rather than an unknown-session answer.
func saveLeaseTestSession(t *testing.T, dir, id string) string {
	t.Helper()
	path := transcriptPath(dir, id)
	s := sessionstore.NewSession("sys")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "editor session"})
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	return path
}

// requireLeaseRefusal holds one lease refusal to the pair docs/ACP.md promises:
// the JSON-RPC code for the class, the identity in data.code, and the human
// message clients still show.
func requireLeaseRefusal(t *testing.T, f frame, wantCode int, wantIdentity string) {
	t.Helper()
	if f.Error == nil {
		t.Fatalf("response %s, want a lease refusal", f.Result)
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

// TestEveryLeaseRefusalCarriesItsDocumentedCode holds both lease answers to the
// documented pair. Wrapped, because that is how they arrive: the store or a
// caller adds what it was doing, and a switch reading the sentence stops
// matching there.
func TestEveryLeaseRefusalCarriesItsDocumentedCode(t *testing.T) {
	cases := []struct {
		name     string
		err      error
		code     int
		identity string
	}{
		{
			name:     "held",
			err:      fmt.Errorf("acquire transcript: %w", sessionstore.ErrSessionLeaseHeld),
			code:     ErrSessionLeaseHeld,
			identity: "busy.session_in_use",
		},
		{
			name:     "unprovable",
			err:      fmt.Errorf("%w: %w", sessionstore.ErrSessionLeaseUnprovable, os.ErrPermission),
			code:     ErrSessionLeaseUnprovable,
			identity: "session.lease_unprovable",
		},
	}
	seen := map[string]struct{}{}
	for _, tc := range cases {
		got := sessionLeaseRefusal("session/load", tc.err)
		data, ok := got.Data.(SessionLeaseRefusalData)
		if !ok || data.Code != tc.identity || got.Code != tc.code {
			t.Fatalf("%s: refusal = %d/%q, want %d/%q", tc.name, got.Code, data.Code, tc.code, tc.identity)
		}
		if got.Message == "" {
			t.Fatalf("%s: refusal carries no message; clients still show it", tc.name)
		}
		if _, dup := seen[data.Code]; dup {
			t.Fatalf("%q names two lease conditions; a client cannot tell them apart", data.Code)
		}
		seen[data.Code] = struct{}{}
	}

	// A failure the lease cannot name is a fault, not a refusal: no code would
	// say the session refused when the acquisition could not run at all.
	unnamed := sessionLeaseRefusal("session/load", errors.New("disk went away"))
	if unnamed.Code != ErrInternal || unnamed.Data != nil {
		t.Fatalf("unnamed failure = %d/%+v, want %d with no data", unnamed.Code, unnamed.Data, ErrInternal)
	}
}

// TestSessionLoadRefusedWhenLeaseHeld is the red-first case: this answer used to
// be -32600, the malformed-frame code, with prose and no data, so a client could
// not tell a session another runtime held from a request it had broken.
func TestSessionLoadRefusedWhenLeaseHeld(t *testing.T) {
	dir := testenv.TempDir(t)
	const id = "sess-held-lease"
	path := saveLeaseTestSession(t, dir, id)

	holder, err := sessionstore.TryAcquireSessionLease(path)
	if err != nil {
		t.Fatalf("test holder acquire: %v", err)
	}
	defer holder.Release()

	info, err := sessionstore.LoadSessionLeaseInfo(path)
	if err != nil {
		t.Fatalf("load holder info: %v", err)
	}
	wantMessage := "session/load: " +
		control.SessionInUseMessage(&sessionstore.SessionLeaseError{Path: path, Info: info}) +
		"; " + control.SessionLeaseCloseHint

	client, stop := startServer(t, leaseTestFactory(dir))
	defer stop()
	client.call(t, "initialize", InitializeParams{ProtocolVersion: 1})

	resp := client.call(t, "session/load", SessionLoadParams{SessionID: id, Cwd: testenv.TempDir(t)})
	requireLeaseRefusal(t, resp, ErrSessionLeaseHeld, "busy.session_in_use")
	if resp.Error.Message != wantMessage {
		t.Fatalf("session/load refusal message = %q, want the exact payload %q", resp.Error.Message, wantMessage)
	}
	if got := string(resp.Error.Data); got != `{"code":"busy.session_in_use"}` {
		t.Fatalf("session/load refusal data = %s, want the documented member", got)
	}
	if strings.Contains(resp.Error.Message, path) {
		t.Fatalf("session/load error leaks the session path: %q", resp.Error.Message)
	}

	// The refused load must not have bound the session: a prompt is unknown.
	promptResp := client.call(t, "session/prompt", SessionPromptParams{
		SessionID: id,
		Prompt:    []ContentBlock{{Type: "text", Text: "should not run"}},
	})
	if promptResp.Error == nil {
		t.Fatalf("session/prompt after refused load succeeded, want unknown session")
	}
}

// TestSessionLoadRefusedWhenLeaseUnprovable is the second red-first case: an
// ownership probe that could not decide used to collapse into -32603, so a
// client could not tell missing evidence from a disk that would not write — and
// the answer must still not claim a holder the store never observed.
func TestSessionLoadRefusedWhenLeaseUnprovable(t *testing.T) {
	dir := testenv.TempDir(t)
	const id = "sess-unprovable-lease"
	path := saveLeaseTestSession(t, dir, id)
	// A directory where the lease lock belongs: opening it fails, so the probe
	// cannot prove either answer. Making it a directory keeps the scenario
	// deterministic even for a privileged runner that ignores file modes.
	lock := store.SessionLeaseLock(sessionstore.CanonicalSessionPath(path))
	if err := os.Mkdir(lock, 0o755); err != nil {
		t.Fatalf("make the lease lock unopenable: %v", err)
	}

	client, stop := startServer(t, leaseTestFactory(dir))
	defer stop()
	client.call(t, "initialize", InitializeParams{ProtocolVersion: 1})

	resp := client.call(t, "session/load", SessionLoadParams{SessionID: id, Cwd: testenv.TempDir(t)})
	requireLeaseRefusal(t, resp, ErrSessionLeaseUnprovable, "session.lease_unprovable")
	if got := string(resp.Error.Data); got != `{"code":"session.lease_unprovable"}` {
		t.Fatalf("session/load refusal data = %s, want the documented member", got)
	}
	if !strings.Contains(resp.Error.Message, "could not be proven") {
		t.Fatalf("unprovable refusal = %q, want the human message kept", resp.Error.Message)
	}
	if strings.Contains(resp.Error.Message, "in use") || strings.Contains(resp.Error.Message, "already open") {
		t.Fatalf("unprovable lease reported as held: %q", resp.Error.Message)
	}

	// It must not have bound the session either.
	promptResp := client.call(t, "session/prompt", SessionPromptParams{
		SessionID: id,
		Prompt:    []ContentBlock{{Type: "text", Text: "should not run"}},
	})
	if promptResp.Error == nil {
		t.Fatalf("session/prompt after refused load succeeded, want unknown session")
	}
}

// TestSessionLeaseAcquireFailureStaysAFaultWhenTheProbeIsFree is the boundary
// between the two mappers: a raw acquisition error over a session nobody holds
// is a fault, not an invented refusal.
func TestSessionLeaseAcquireFailureStaysAFaultWhenTheProbeIsFree(t *testing.T) {
	path := filepath.Join(testenv.TempDir(t), "nobody-holds-this.jsonl")
	got := sessionLeaseAcquireRefusal("session/load", path, errors.New("open lock: boom"))
	if got.Code != ErrInternal || got.Data != nil {
		t.Fatalf("failure = %d/%+v, want %d with no data", got.Code, got.Data, ErrInternal)
	}
}

// TestSessionLoadSuccessIsUnchanged is the control: a session nobody holds opens
// exactly as it did before, with no error payload at all.
func TestSessionLoadSuccessIsUnchanged(t *testing.T) {
	dir := testenv.TempDir(t)
	const id = "sess-free-lease"
	saveLeaseTestSession(t, dir, id)

	client, stop := startServer(t, leaseTestFactory(dir))
	defer stop()
	client.call(t, "initialize", InitializeParams{ProtocolVersion: 1})

	resp := client.call(t, "session/load", SessionLoadParams{SessionID: id, Cwd: testenv.TempDir(t)})
	if resp.Error != nil {
		t.Fatalf("session/load of a free transcript refused: %s", wireError(resp))
	}
	var state SessionConfigState
	if err := json.Unmarshal(resp.Result, &state); err != nil {
		t.Fatalf("session/load result %s: %v", resp.Result, err)
	}
}

// TestSessionCloseReleasesLease proves the lease taken by session/new is
// released by session/close, so another runtime can bind the transcript.
func TestSessionCloseReleasesLease(t *testing.T) {
	dir := testenv.TempDir(t)
	client, stop := startServer(t, leaseTestFactory(dir))
	defer stop()

	sid := openSession(t, client)
	path := transcriptPath(dir, sid)

	// Held while the session is open.
	if lease, err := sessionstore.TryAcquireSessionLease(path); err == nil {
		lease.Release()
		t.Fatalf("transcript lease not held after session/new")
	}

	resp := client.call(t, "session/close", SessionCloseParams{SessionID: sid})
	if resp.Error != nil {
		t.Fatalf("session/close errored: %+v", resp.Error)
	}
	lease, err := sessionstore.TryAcquireSessionLease(path)
	if err != nil {
		t.Fatalf("transcript lease not released by session/close: %v", err)
	}
	lease.Release()
}
