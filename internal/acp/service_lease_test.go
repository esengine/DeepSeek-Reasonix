package acp

import (
	"strings"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/permission"
	"reasonix/internal/provider"
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

// TestSessionLoadRefusedWhenLeaseHeld proves session/load returns a protocol
// error naming the holder when the transcript is owned by another runtime
// (a desktop window or CLI writing the same session), and does not register
// the session.
func TestSessionLoadRefusedWhenLeaseHeld(t *testing.T) {
	dir := t.TempDir()
	const id = "sess-held-lease"
	path := transcriptPath(dir, id)
	s := agent.NewSession("sys")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "editor session"})
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}

	holder, err := agent.TryAcquireSessionLease(path)
	if err != nil {
		t.Fatalf("test holder acquire: %v", err)
	}
	defer holder.Release()

	client, stop := startServer(t, leaseTestFactory(dir))
	defer stop()
	client.call(t, "initialize", InitializeParams{ProtocolVersion: 1})

	resp := client.call(t, "session/load", SessionLoadParams{SessionID: id, Cwd: t.TempDir()})
	if resp.Error == nil {
		t.Fatalf("session/load of a held session succeeded, want protocol error")
	}
	if !strings.Contains(resp.Error.Message, "in use by another Reasonix") {
		t.Fatalf("session/load error = %q, want holder wording", resp.Error.Message)
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

// TestSessionCloseReleasesLease proves the lease taken by session/new is
// released by session/close, so another runtime can bind the transcript.
func TestSessionCloseReleasesLease(t *testing.T) {
	dir := t.TempDir()
	client, stop := startServer(t, leaseTestFactory(dir))
	defer stop()

	sid := openSession(t, client)
	path := transcriptPath(dir, sid)

	// Held while the session is open.
	if lease, err := agent.TryAcquireSessionLease(path); err == nil {
		lease.Release()
		t.Fatalf("transcript lease not held after session/new")
	}

	resp := client.call(t, "session/close", SessionCloseParams{SessionID: sid})
	if resp.Error != nil {
		t.Fatalf("session/close errored: %+v", resp.Error)
	}
	lease, err := agent.TryAcquireSessionLease(path)
	if err != nil {
		t.Fatalf("transcript lease not released by session/close: %v", err)
	}
	lease.Release()
}
