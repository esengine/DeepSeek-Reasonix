package installsource

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"reasonix/internal/base/testenv"
)

const pinnedCommit = "0123456789abcdef0123456789abcdef01234567"

func skillServer(t *testing.T, body *atomic.Value) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body.Load().(string)))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func execRaw(t *testing.T, tl interface {
	Execute(context.Context, json.RawMessage) (string, error)
}, args map[string]any) (response, error) {
	t.Helper()
	raw, _ := json.Marshal(args)
	out, err := tl.Execute(context.Background(), raw)
	if err != nil {
		return response{}, err
	}
	var resp response
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatalf("response JSON %q: %v", out, err)
	}
	return resp, nil
}

// The digest is what a reviewer records on one machine and an installer checks
// on another, so where each of them would write the skill cannot move it.
func TestContentDigestIgnoresWhereThePlanWouldWrite(t *testing.T) {
	var body atomic.Value
	body.Store("---\nname: pinned\ndescription: pinned skill\n---\nbody v1")
	srv := skillServer(t, &body)

	a := NewTool(Options{ProjectRoot: testenv.TempDir(t), HomeDir: testenv.TempDir(t), HTTPClient: srv.Client()})
	b := NewTool(Options{ProjectRoot: testenv.TempDir(t), HomeDir: testenv.TempDir(t), HTTPClient: srv.Client()})
	args := map[string]any{"source": srv.URL + "/SKILL.md", "kind": "skill"}
	ra := execInstall(t, a, args)
	rb := execInstall(t, b, map[string]any{"source": srv.URL + "/SKILL.md", "kind": "skill", "scope": "global"})
	if !IsContentDigest(ra.ContentDigest) {
		t.Fatalf("plan carries no digest: %+v", ra)
	}
	if ra.ContentDigest != rb.ContentDigest {
		t.Fatalf("digest moved with the install location: %s vs %s", ra.ContentDigest, rb.ContentDigest)
	}
}

// A pinned apply installs only the reviewed bytes: once the source serves
// anything else, nothing is written and the refusal carries its identity.
func TestExpectDigestRefusesChangedContentAndWritesNothing(t *testing.T) {
	var body atomic.Value
	body.Store("---\nname: pinned\ndescription: pinned skill\n---\nbody v1")
	srv := skillServer(t, &body)
	home := testenv.TempDir(t)
	tl := NewTool(Options{ProjectRoot: testenv.TempDir(t), HomeDir: home, HTTPClient: srv.Client(), RequireApprovedPlan: true})
	source := srv.URL + "/SKILL.md"

	plan := execInstall(t, tl, map[string]any{"source": source, "kind": "skill", "scope": "global"})
	reviewed := plan.ContentDigest

	body.Store("---\nname: pinned\ndescription: pinned skill\n---\nbody v2 with new instructions")
	_, err := execRaw(t, tl, map[string]any{
		"source": source, "kind": "skill", "scope": "global", "apply": true,
		"planId": plan.PlanID, "expectDigest": reviewed,
	})
	if !errors.Is(err, ErrDigestMismatch) {
		t.Fatalf("apply against changed content: err = %v, want ErrDigestMismatch", err)
	}
	if _, statErr := os.Stat(filepath.Join(home, ".reasonix", "skills", "pinned")); !os.IsNotExist(statErr) {
		t.Fatalf("a refused pinned apply still wrote the skill: %v", statErr)
	}

	body.Store("---\nname: pinned\ndescription: pinned skill\n---\nbody v1")
	done, err := execRaw(t, tl, map[string]any{
		"source": source, "kind": "skill", "scope": "global", "apply": true,
		"planId": plan.PlanID, "expectDigest": reviewed,
	})
	if err != nil || done.Status != "done" {
		t.Fatalf("apply of the reviewed bytes: resp=%+v err=%v", done, err)
	}
	got, _ := os.ReadFile(filepath.Join(home, ".reasonix", "skills", "pinned", "SKILL.md"))
	if !strings.Contains(string(got), "body v1") {
		t.Fatalf("installed %q, not the reviewed body", got)
	}
}

// A local folder can change after review, so it has no digest to match.
func TestExpectDigestRefusesUnpinnableSource(t *testing.T) {
	dir := testenv.TempDir(t)
	writeFile(t, filepath.Join(dir, "SKILL.md"), "---\nname: local\ndescription: local skill\n---\nbody")
	tl := NewTool(Options{ProjectRoot: testenv.TempDir(t), HomeDir: testenv.TempDir(t)})
	plan := execInstall(t, tl, map[string]any{"source": dir, "kind": "skill"})
	if plan.ContentDigest != "" {
		t.Fatalf("a local folder was given a digest: %s", plan.ContentDigest)
	}
	_, err := execRaw(t, tl, map[string]any{"source": dir, "kind": "skill", "expectDigest": "sha256:" + strings.Repeat("a", 64)})
	if !errors.Is(err, ErrNotPinnable) {
		t.Fatalf("err = %v, want ErrNotPinnable", err)
	}
}

// Git revision identity remains part of approval even for identical snapshots.
func TestExpectDigestPinsPluginCommit(t *testing.T) {
	src := testenv.TempDir(t)
	writeFile(t, filepath.Join(src, ".claude-plugin", "plugin.json"), `{"name": "pwf", "version": "1.0.0"}`)
	writeFile(t, filepath.Join(src, "skills", "planner", "SKILL.md"), "---\ndescription: planner\n---\nbody")
	commit := pinnedCommit
	tl := NewTool(Options{ProjectRoot: testenv.TempDir(t), HomeDir: testenv.TempDir(t)})
	tl.preparePlugin = func(context.Context, string, string) (string, string, func(), error) {
		return src, commit, func() {}, nil
	}
	args := map[string]any{"source": "https://github.com/acme/pwf/tree/" + pinnedCommit, "kind": "plugin"}
	plan := execInstall(t, tl, args)
	if !IsContentDigest(plan.ContentDigest) {
		t.Fatalf("plugin plan carries no digest: %+v", plan)
	}
	args["expectDigest"] = plan.ContentDigest
	if _, err := execRaw(t, tl, args); err != nil {
		t.Fatalf("same commit refused: %v", err)
	}
	commit = strings.Repeat("f", 40)
	if _, err := execRaw(t, tl, args); !errors.Is(err, ErrDigestMismatch) {
		t.Fatalf("moved commit: err = %v, want ErrDigestMismatch", err)
	}
	commit = ""
	if _, err := execRaw(t, tl, args); !errors.Is(err, ErrDigestMismatch) {
		t.Fatalf("changed snapshot identity: err = %v, want ErrDigestMismatch", err)
	}
}

// What an MCP entry starts is the material; changing the command changes it.
func TestContentDigestCoversMCPEntry(t *testing.T) {
	var body atomic.Value
	body.Store(`{"mcpServers":{"demo":{"command":"npx","args":["-y","demo-mcp@1.0.0"]}}}`)
	srv := skillServer(t, &body)
	tl := NewTool(Options{ProjectRoot: testenv.TempDir(t), HomeDir: testenv.TempDir(t), HTTPClient: srv.Client()})
	args := map[string]any{"source": srv.URL + "/.mcp.json", "kind": "mcp"}
	first := execInstall(t, tl, args)
	body.Store(`{"mcpServers":{"demo":{"command":"npx","args":["-y","demo-mcp@2.0.0"]}}}`)
	second := execInstall(t, tl, args)
	if !IsContentDigest(first.ContentDigest) || first.ContentDigest == second.ContentDigest {
		t.Fatalf("digest did not follow the entry: %q then %q", first.ContentDigest, second.ContentDigest)
	}
}

func TestIsContentDigest(t *testing.T) {
	for s, want := range map[string]bool{
		"sha256:" + strings.Repeat("0", 64): true,
		"sha256:" + strings.Repeat("A", 64): false,
		"sha256:" + strings.Repeat("0", 63): false,
		strings.Repeat("0", 64):             false,
		"":                                  false,
	} {
		if IsContentDigest(s) != want {
			t.Errorf("IsContentDigest(%q) = %v", s, !want)
		}
	}
}
