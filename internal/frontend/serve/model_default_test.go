package serve

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/session/control"
	"reasonix/internal/state/sessionstore"
)

func twoModelServer(t *testing.T) (*httptest.Server, string) {
	srv, path, _, _ := twoModelServerAt(t)
	return srv, path
}

func twoModelServerAt(t *testing.T) (*httptest.Server, string, *Server, string) {
	t.Helper()
	writeServeEffortSelectionConfig(t, "high")
	path := config.UserConfigPath()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	edited := strings.Replace(string(raw), `models = ["shared-chat"]`, `models = ["shared-chat", "other-chat"]`, 1)
	if err := os.WriteFile(path, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := testenv.TempDir(t)
	bc := NewBroadcaster()
	ctrl := control.New(control.Options{Sink: bc, ModelRef: "alternate/shared-chat", SessionDir: dir})
	t.Cleanup(ctrl.Close)
	server := New(ctrl, bc, config.ServeConfig{})
	server.buildController = func(_ context.Context, ref string) (*control.Controller, error) {
		return control.New(control.Options{Sink: bc, ModelRef: ref, SessionDir: dir}), nil
	}
	srv := httptest.NewServer(operatorHandler(server))
	t.Cleanup(srv.Close)
	return srv, path, server, dir
}

func postModel(t *testing.T, base, body string) {
	t.Helper()
	resp, err := http.Post(base+"/model", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("POST /model %s = %d", body, resp.StatusCode)
	}
}

// A switch inside a session is that session's choice; it must not move the
// default every other session and the next launch start from.
func TestModelSwitchLeavesDefaultModelAlone(t *testing.T) {
	srv, path := twoModelServer(t)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	postModel(t, srv.URL, `{"ref":"alternate/other-chat"}`)

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatalf("a session switch rewrote the user config:\n%s", after)
	}
}

func postDefaultModel(t *testing.T, base, body string) *http.Response {
	t.Helper()
	resp, err := http.Post(base+"/default-model", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func TestDefaultModelPersistsWithoutSwitchingTheSession(t *testing.T) {
	srv, path, server, _ := twoModelServerAt(t)

	resp := postDefaultModel(t, srv.URL, `{"ref":"alternate/other-chat"}`)

	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("POST /default-model = %d", resp.StatusCode)
	}
	if got := config.LoadForEdit(path).DefaultModel; got != "alternate/other-chat" {
		t.Fatalf("default_model = %q, want alternate/other-chat", got)
	}
	if got := server.ctl().ModelRef(); got != "alternate/shared-chat" {
		t.Fatalf("session model = %q: setting the default switched the session", got)
	}
}

func TestDefaultModelRefusesAnUnknownModel(t *testing.T) {
	srv, path := twoModelServer(t)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	resp := postDefaultModel(t, srv.URL, `{"ref":"gone/removed"}`)

	if resp.StatusCode != http.StatusBadRequest || reasonCode(t, resp) != "settings.unknown_model" {
		t.Fatalf("POST /default-model = %d, want 400 settings.unknown_model", resp.StatusCode)
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatalf("a refused default rewrote the config:\n%s", after)
	}
}

// A pane resolving through a broker has no default_model of its own to move:
// the default it shows is the home machine's, so the refusal says so instead of
// reporting a write that landed on a file nobody reads.
func TestDefaultModelOnABrokeredPaneIsRefusedByIdentity(t *testing.T) {
	srv, path, server, _ := twoModelServerAt(t)
	server.resolver = homeResolver()
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	resp := postDefaultModel(t, srv.URL, `{"ref":"home/chat-a"}`)

	if resp.StatusCode != http.StatusConflict || reasonCode(t, resp) != "settings.default_model_brokered" {
		t.Fatalf("POST /default-model = %d, want 409 settings.default_model_brokered", resp.StatusCode)
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatalf("a brokered pane wrote this machine's config:\n%s", after)
	}
}

func savedSession(t *testing.T, dir, model string) string {
	t.Helper()
	path := sessionstore.NewSessionPath(dir, "test")
	sess := sessionstore.NewSession("")
	if err := sess.SaveIfAbsent(path); err != nil {
		t.Fatal(err)
	}
	if err := sessionstore.SaveBranchMeta(path, sessionstore.BranchMeta{Model: model, SchemaVersion: sessionstore.BranchMetaCountsVersion}); err != nil {
		t.Fatal(err)
	}
	return path
}

func resumeModel(t *testing.T, saved string) string {
	t.Helper()
	srv, _, server, dir := twoModelServerAt(t)
	path := savedSession(t, dir, saved)
	resp, err := http.Post(srv.URL+"/resume", "application/json", strings.NewReader(`{"path":`+strconv.Quote(path)+`}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("POST /resume = %d", resp.StatusCode)
	}
	return server.ctl().ModelRef()
}

// Reopening a session brings it back on the model it was left on, not on
// whatever the window happens to run.
func TestResumeRestoresTheSessionsOwnModel(t *testing.T) {
	if got := resumeModel(t, "alternate/other-chat"); got != "alternate/other-chat" {
		t.Fatalf("resumed on %q, want alternate/other-chat", got)
	}
}

func TestResumeWithoutRecordedModelKeepsTheCurrentOne(t *testing.T) {
	if got := resumeModel(t, ""); got != "alternate/shared-chat" {
		t.Fatalf("resumed on %q, want the current alternate/shared-chat", got)
	}
}

func TestResumeWithUnknownRecordedModelFallsBack(t *testing.T) {
	if got := resumeModel(t, "gone/removed"); got != "alternate/shared-chat" {
		t.Fatalf("resumed on %q, want the current alternate/shared-chat", got)
	}
}

// A session another runtime holds opens read-only and cannot be rebuilt onto a
// new model, so the window keeps the one it had.
func TestResumeOfAHeldSessionKeepsTheCurrentModel(t *testing.T) {
	srv, _, server, dir := twoModelServerAt(t)
	path := savedSession(t, dir, "alternate/other-chat")
	holder, err := sessionstore.TryAcquireSessionLease(path)
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Release()
	leases := control.NewSessionLeaseKeeper()
	defer leases.Release()
	if err := server.SetSessionLeases(leases); err != nil {
		t.Fatal(err)
	}
	resp, err := http.Post(srv.URL+"/resume", "application/json", strings.NewReader(`{"path":`+strconv.Quote(path)+`}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("POST /resume = %d", resp.StatusCode)
	}
	if got := server.ctl().ModelRef(); got != "alternate/shared-chat" {
		t.Fatalf("held session resumed on %q, want the current alternate/shared-chat", got)
	}
}
