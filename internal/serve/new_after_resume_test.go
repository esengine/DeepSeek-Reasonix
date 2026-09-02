package serve

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/config"
	"reasonix/internal/control"
)

// TestNewSessionAfterResumeKeepsWritePath reproduces the field report: after
// an in-place /resume onto a listed session, POST /new must still rotate.
// Rebind's releaseLocked used to strip the controller's transition handler
// while Resume left the session authority-required, so the next /new failed
// with "bind new session: session write authority missing".
func TestNewSessionAfterResumeKeepsWritePath(t *testing.T) {
	dir := t.TempDir()
	aPath := filepath.Join(dir, "a.jsonl")
	bPath := filepath.Join(dir, "b.jsonl")
	saveServeTestSession(t, aPath)
	saveServeTestSession(t, bPath)

	bc := NewBroadcaster()
	exec := agent.New(nil, nil, agent.NewSession("sys"), agent.Options{}, bc)
	ctrl := control.New(control.Options{Executor: exec, Sink: bc, SessionDir: dir, SessionPath: aPath})
	server := New(ctrl, bc, config.ServeConfig{})
	leases := control.NewSessionLeaseKeeper()
	defer leases.Release()
	if err := leases.Rebind(aPath); err != nil {
		t.Fatal(err)
	}
	if err := server.SetSessionLeases(leases); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(server.Handler())
	defer srv.Close()

	resumeBody, _ := json.Marshal(map[string]string{"path": bPath})
	resp, err := http.Post(srv.URL+"/resume", "application/json", bytes.NewReader(resumeBody))
	if err != nil {
		t.Fatal(err)
	}
	resumeRespBody, _ := readAll(resp)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("/resume status = %d body %q, want 204", resp.StatusCode, resumeRespBody)
	}

	newResp, err := http.Post(srv.URL+"/new", "application/json", bytes.NewReader([]byte(`{}`)))
	if err != nil {
		t.Fatal(err)
	}
	newRespBody, _ := readAll(newResp)
	if newResp.StatusCode != http.StatusNoContent {
		t.Fatalf("/new after resume status = %d body %q, want 204", newResp.StatusCode, newRespBody)
	}
}
