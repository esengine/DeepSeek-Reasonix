package control

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/tools/builtin"
)

func droppedGrantConsumers(t *testing.T) (*Controller, *builtin.PathResolver, string) {
	t.Helper()
	workspace := t.TempDir()
	paths := builtin.NewPathResolver()
	c := New(Options{WorkspaceRoot: workspace, ExternalFolderToolRefs: paths})
	t.Cleanup(c.Close)
	return c, paths, workspace
}

func writeGrantFixture(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func readDroppedGrant(t *testing.T, workspace string, paths *builtin.PathResolver, path string, readRoots []string) (string, error) {
	t.Helper()
	tools := (builtin.Workspace{Dir: workspace, ReadPaths: paths, ReadRoots: readRoots}).Tools("read_file")
	if len(tools) != 1 {
		t.Fatalf("expected read_file tool, got %d tools", len(tools))
	}
	args, err := json.Marshal(map[string]string{"path": path})
	if err != nil {
		t.Fatal(err)
	}
	return tools[0].Execute(context.Background(), args)
}

func droppedGrantReadScope(t *testing.T, workspace string, paths *builtin.PathResolver, token string) []string {
	t.Helper()
	resolved, ok := paths.Resolve(token)
	if !ok || !resolved.External {
		t.Fatalf("dropped reference did not register its tool read root: %+v", resolved)
	}
	return []string{workspace, resolved.Root}
}

func assertDroppedGrantRefs(t *testing.T, c *Controller, token, content string, allowed bool) {
	t.Helper()
	for name, resolve := range map[string]func(context.Context, string) (string, []string){
		"scoped":   c.ResolveScopedRefs,
		"ordinary": c.ResolveRefs,
	} {
		t.Run(name, func(t *testing.T) {
			block, errs := resolve(context.Background(), "inspect @"+EscapeRefPath(token))
			if allowed {
				if len(errs) != 0 || !strings.Contains(block, content) {
					t.Fatalf("selected content unavailable: block=%q errors=%v", block, errs)
				}
			} else if block != "" || strings.Contains(strings.Join(errs, "\n"), content) {
				t.Fatalf("unauthorized bytes reached @reference resolution: block=%q errors=%v", block, errs)
			}
		})
	}
}

func TestDroppedSingleFileGrantRejectsSiblingAcrossConsumers(t *testing.T) {
	c, paths, workspace := droppedGrantConsumers(t)
	outside := t.TempDir()
	selected := filepath.Join(outside, "selected.txt")
	sibling := filepath.Join(outside, "sibling.txt")
	const selectedBody = "SELECTED-FILE-CONTENT"
	const siblingBody = "UNSELECTED-SIBLING-PRIVATE-CONTENT"
	writeGrantFixture(t, selected, selectedBody)
	writeGrantFixture(t, sibling, siblingBody)
	escapedToken, _, err := c.DroppedRef(selected)
	if err != nil {
		t.Fatal(err)
	}
	token := UnescapeRefPath(escapedToken)
	readRoots := droppedGrantReadScope(t, workspace, paths, token)
	assertDroppedGrantRefs(t, c, token, selectedBody, true)
	if out, err := readDroppedGrant(t, workspace, paths, token, readRoots); err != nil || !strings.Contains(out, selectedBody) {
		t.Fatalf("selected file unavailable to read_file: output=%q error=%v", out, err)
	}
	if _, _, ok := c.ExternalFolderRefLocalPath(token); !ok {
		t.Fatal("selected file unavailable to local-path consumer")
	}
	for name, denied := range map[string]string{
		"sibling":   strings.TrimSuffix(token, "/selected.txt") + "/sibling.txt",
		"traversal": token + "/../sibling.txt",
	} {
		t.Run(name, func(t *testing.T) {
			assertDroppedGrantRefs(t, c, denied, siblingBody, false)
			if path, _, ok := c.ExternalFolderRefLocalPath(denied); ok {
				t.Errorf("local-path consumer widened a file grant: %q", path)
			}
			if resolved, ok := paths.Resolve(denied); ok {
				t.Errorf("tool resolver widened a file grant: %+v", resolved)
			}
			if out, err := readDroppedGrant(t, workspace, paths, denied, readRoots); err == nil || strings.Contains(out, siblingBody) {
				t.Errorf("read_file accepted unselected sibling: output=%q error=%v", out, err)
			}
		})
	}
	if block, errs := c.ResolveScopedRefs(context.Background(), "inspect @"+EscapeRefPath(filepath.ToSlash(sibling))); block != "" {
		t.Fatalf("absolute sibling resolved without a grant: block=%q errors=%v", block, errs)
	}
	if out, err := readDroppedGrant(t, workspace, paths, sibling, readRoots); err == nil || strings.Contains(out, siblingBody) {
		t.Fatalf("registered file root authorized its absolute sibling: output=%q error=%v", out, err)
	}
}

func TestDroppedDirectoryGrantRejectsEscapingLinkAcrossConsumers(t *testing.T) {
	c, paths, workspace := droppedGrantConsumers(t)
	allowed, outside := t.TempDir(), t.TempDir()
	const childBody = "SELECTED-DIRECTORY-DESCENDANT"
	const outsideBody = "OUTSIDE-DIRECTORY-PRIVATE-CONTENT"
	writeGrantFixture(t, filepath.Join(allowed, "sub", "child.txt"), childBody)
	writeGrantFixture(t, filepath.Join(outside, "secret.txt"), outsideBody)
	attachmentStorageDirectoryLink(t, outside, filepath.Join(allowed, "escape"))
	token, _, err := c.DroppedRef(allowed)
	if err != nil {
		t.Fatal(err)
	}
	childToken := token + "/sub/child.txt"
	readRoots := droppedGrantReadScope(t, workspace, paths, token)
	assertDroppedGrantRefs(t, c, childToken, childBody, true)
	if out, err := readDroppedGrant(t, workspace, paths, childToken, readRoots); err != nil || !strings.Contains(out, childBody) {
		t.Fatalf("directory descendant unavailable: output=%q error=%v", out, err)
	}
	for name, denied := range map[string]string{
		"link":      token + "/escape/secret.txt",
		"traversal": token + "/../" + filepath.Base(outside) + "/secret.txt",
	} {
		t.Run(name, func(t *testing.T) {
			assertDroppedGrantRefs(t, c, denied, outsideBody, false)
			out, err := readDroppedGrant(t, workspace, paths, denied, readRoots)
			if err == nil || strings.Contains(out, outsideBody) {
				t.Errorf("read_file followed an unauthorized external path: output=%q error=%v", out, err)
			}
			if path, _, ok := c.ExternalFolderRefLocalPath(denied); ok {
				out, err := readDroppedGrant(t, workspace, paths, path, readRoots)
				if err == nil || strings.Contains(out, outsideBody) {
					t.Errorf("local-path consumer bypassed the actual read scope: output=%q error=%v", out, err)
				}
			}
		})
	}
	if block, errs := c.ResolveScopedRefs(context.Background(), "inspect @"+EscapeRefPath(filepath.ToSlash(filepath.Join(outside, "secret.txt")))); block != "" {
		t.Fatalf("unselected external file resolved: block=%q errors=%v", block, errs)
	}
	if out, err := readDroppedGrant(t, workspace, paths, filepath.Join(outside, "secret.txt"), readRoots); err == nil || strings.Contains(out, outsideBody) {
		t.Fatalf("directory grant authorized an unselected absolute file: output=%q error=%v", out, err)
	}
}
