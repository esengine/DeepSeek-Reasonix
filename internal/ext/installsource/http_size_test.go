package installsource

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
)

func TestFetchTextSizeBoundary(t *testing.T) {
	for _, streamed := range []bool{false, true} {
		for _, size := range []int{defaultFetchLimit - 1, defaultFetchLimit, defaultFetchLimit + 1} {
			t.Run(fmt.Sprintf("streamed=%t/bytes=%d", streamed, size), func(t *testing.T) {
				body := strings.Repeat("x", size)
				srv := manifestSizeServer(t, body, streamed)
				tl := NewTool(Options{ProjectRoot: testenv.TempDir(t), HomeDir: testenv.TempDir(t), HTTPClient: srv.Client()})
				got, err := tl.fetchText(context.Background(), srv.URL+"/SKILL.md")
				if size > defaultFetchLimit {
					if !errors.Is(err, ErrSourceUnreadable) || got != "" {
						t.Fatalf("oversized read: bytes=%d err=%v; want no content and ErrSourceUnreadable", len(got), err)
					}
					return
				}
				if err != nil || got != body {
					t.Fatalf("bounded read: bytes=%d err=%v; want all %d bytes", len(got), err, size)
				}
			})
		}
	}
}

func TestOversizedDirectManifestReturnsReadError(t *testing.T) {
	for _, tc := range []struct{ kind, path, prefix, pad string }{
		{"auto", "/SKILL.md", "---\nname: large-skill\ndescription: Large skill fixture\n---\n", "x"},
		{"skill", "/SKILL.md", "---\nname: large-skill\ndescription: Large skill fixture\n---\n", "x"},
		{"auto", "/.mcp.json", `{"mcpServers":{"counter":{"url":"https://example.com/mcp"}}}`, " "},
		{"mcp", "/.mcp.json", `{"mcpServers":{"counter":{"url":"https://example.com/mcp"}}}`, " "},
	} {
		t.Run(tc.kind+tc.path, func(t *testing.T) {
			body := tc.prefix + strings.Repeat(tc.pad, defaultFetchLimit+1-len(tc.prefix))
			srv := manifestSizeServer(t, body, true)
			root, home := testenv.TempDir(t), testenv.TempDir(t)
			tl := NewTool(Options{ProjectRoot: root, HomeDir: home, HTTPClient: srv.Client(), RequireApprovedPlan: true})
			raw, err := json.Marshal(map[string]any{"source": srv.URL + tc.path, "kind": tc.kind})
			if err != nil {
				t.Fatal(err)
			}
			out, err := tl.Execute(context.Background(), raw)
			if !errors.Is(err, ErrSourceUnreadable) || out != "" {
				t.Fatalf("oversized %s manifest produced %d response bytes and err=%v; want the read error", tc.kind, len(out), err)
			}
			for _, path := range []string{root, home} {
				entries, err := os.ReadDir(path)
				if err != nil || len(entries) != 0 {
					t.Fatalf("preview wrote to %s: entries=%d err=%v", path, len(entries), err)
				}
			}
		})
	}
}

func TestSkillAtFetchLimitInstallsAllBytes(t *testing.T) {
	prefix := "---\nname: large-skill\ndescription: Large skill fixture\n---\n"
	body := prefix + strings.Repeat("x", defaultFetchLimit-len(prefix)-len("\nEnd.\n")) + "\nEnd.\n"
	srv := manifestSizeServer(t, body, true)
	root, home := testenv.TempDir(t), testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", filepath.Join(home, ".reasonix"))
	tl := NewTool(Options{ProjectRoot: root, HomeDir: home, HTTPClient: srv.Client(), RequireApprovedPlan: true})
	args := map[string]any{"source": srv.URL + "/SKILL.md", "kind": "auto", "mode": "copy", "scope": "global"}
	preview := execInstall(t, tl, args)
	if !preview.OK || preview.Applied || len(preview.Actions) != 1 {
		t.Fatal("limit-sized skill did not produce one unapplied preview")
	}
	args["apply"], args["planId"] = true, preview.PlanID
	out := execInstall(t, tl, args)
	if !out.OK || !out.Applied || len(out.Actions) != 1 {
		t.Fatal("limit-sized skill did not install")
	}
	got, err := os.ReadFile(out.Actions[0].Target)
	if err != nil || string(got) != body {
		t.Fatalf("installed skill: bytes=%d err=%v; want all %d bytes", len(got), err, len(body))
	}
}

func TestManifestReadErrorKeepsGitHubTreeFolderFallback(t *testing.T) {
	oldAPI := githubAPIBaseURL
	githubAPIBaseURL = "https://api.example.test"
	t.Cleanup(func() { githubAPIBaseURL = oldAPI })
	client := &http.Client{Transport: manifestSizeRoundTrip(func(r *http.Request) (*http.Response, error) {
		status, body := http.StatusNotFound, "not a direct manifest"
		switch r.URL.String() {
		case "https://api.example.test/repos/acme/skills/contents/group.md?ref=main":
			status = http.StatusOK
			body = `[{"name":"SKILL.md","path":"group.md/SKILL.md","type":"file","download_url":"https://raw.example.test/SKILL.md"}]`
		case "https://raw.example.test/SKILL.md":
			status = http.StatusOK
			body = "---\nname: folder-skill\ndescription: Directory skill fixture\n---\nRead the project.\n"
		}
		return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})}
	tl := &Tool{root: testenv.TempDir(t), httpClient: client, preparePlugin: func(context.Context, string, string) (string, string, func(), error) {
		return "", "", nil, ErrManifestMissing
	}}
	actions, _, err := tl.planURL(context.Background(), request{
		Source: "https://github.com/acme/skills/tree/main/group.md", Kind: "auto", Mode: "copy", Scope: "project", scopeExplicit: true,
	})
	if err != nil || len(actions) != 1 || actions[0].Kind != "skill" || actions[0].Name != "folder-skill" {
		t.Fatalf("GitHub tree folder ending in .md: actions=%d err=%v; want its skill", len(actions), err)
	}
}

// A GitHub repository whose root .mcp.json is fetched and refused, with nothing
// else to plan, answers with that refusal: neither the remote-endpoint fallback
// (a repo named like "mcp") nor ErrUnsupportedKind (any other name) stands in.
func TestPlanGitHubRepoKeepsTheCauseOfARefusedMCPJSON(t *testing.T) {
	oldAPI := githubAPIBaseURL
	githubAPIBaseURL = "https://api.example.test"
	t.Cleanup(func() { githubAPIBaseURL = oldAPI })
	over := eagerMCPJSON(maxMCPJSONServers + 1)
	want := fmt.Sprintf(".mcp.json declares %d servers; limit is %d", maxMCPJSONServers+1, maxMCPJSONServers)
	for _, repo := range []string{"acme/mcp", "acme/tools"} {
		client := &http.Client{Transport: manifestSizeRoundTrip(func(r *http.Request) (*http.Response, error) {
			status, body := http.StatusNotFound, "not found"
			if r.URL.String() == "https://raw.githubusercontent.com/"+repo+"/main/.mcp.json" {
				status, body = http.StatusOK, over
			}
			return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
		})}
		tl := &Tool{root: testenv.TempDir(t), httpClient: client, preparePlugin: func(context.Context, string, string) (string, string, func(), error) {
			return "", "", nil, ErrManifestMissing
		}}
		actions, _, err := tl.planURL(context.Background(), request{
			Source: "https://github.com/" + repo, Kind: "auto", Mode: "copy", Scope: "project", scopeExplicit: true,
		})
		if !errors.Is(err, ErrInvalidManifest) || !strings.Contains(err.Error(), want) {
			t.Fatalf("auto plan of github.com/%s: %d actions, err = %v; want the refusal saying %q", repo, len(actions), err, want)
		}
	}
}

type manifestSizeRoundTrip func(*http.Request) (*http.Response, error)

func (f manifestSizeRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func manifestSizeServer(t *testing.T, body string, streamed bool) *httptest.Server {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if streamed {
			w.(http.Flusher).Flush()
		} else {
			w.Header().Set("Content-Length", fmt.Sprint(len(body)))
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}
