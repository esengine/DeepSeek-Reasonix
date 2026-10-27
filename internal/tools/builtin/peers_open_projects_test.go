package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"reasonix/internal/contract/tool"
)

type fakeOpener struct {
	fakePeers
	asked []string
	out   []tool.OpenedProject
	err   error
}

func (f *fakeOpener) OpenProjects(_ context.Context, paths []string) ([]tool.OpenedProject, error) {
	f.asked = paths
	return f.out, f.err
}

func openTool(dir tool.PeerDirectory) tool.Tool {
	link := &tool.PeerLink{}
	if dir != nil {
		link.Bind(dir)
	}
	return OpenProjectsTool(link)
}

func TestOpenProjectsToolReportsEachFolderAndDeclaresItsScope(t *testing.T) {
	f := &fakeOpener{out: []tool.OpenedProject{
		{Path: "/w/1", Added: true},
		{Path: "/w/2"},
		{Path: "/w/3", Code: "workspace_roots.too_broad"},
	}}
	tl := openTool(f)
	if got := tool.ApprovalScopeOf(tl); got != tool.ApprovalScopeOpenProjects {
		t.Fatalf("scope = %q", got)
	}
	out, err := tl.Execute(context.Background(), json.RawMessage(`{"paths":["/w/1","/w/2","/w/3"]}`))
	if err != nil || len(f.asked) != 3 {
		t.Fatalf("out %q err %v asked %v", out, err, f.asked)
	}
	for _, want := range []string{"/w/1: opened as a project", "/w/2: already in the project list", "/w/3: refused (workspace_roots.too_broad)", "dispatch_to_project"} {
		if !strings.Contains(out, want) {
			t.Errorf("result lacks %q:\n%s", want, out)
		}
	}
}

func TestOpenProjectsToolPassesTheRefusalThroughAndNeedsAHost(t *testing.T) {
	refused := tool.Refusal{Code: "open_projects.declined", Message: "no"}
	if _, err := openTool(&fakeOpener{err: refused}).Execute(context.Background(), json.RawMessage(`{"paths":["/w/1"]}`)); !errors.As(err, new(tool.Refusal)) {
		t.Fatalf("err = %v, want the refusal kept as an identity", err)
	}
	if _, err := openTool(&fakePeers{}).Execute(context.Background(), json.RawMessage(`{"paths":["/w/1"]}`)); err == nil || !strings.Contains(err.Error(), "dispatch.unavailable") {
		t.Fatalf("a directory that cannot open projects = %v", err)
	}
	if _, err := openTool(nil).Execute(context.Background(), json.RawMessage(`{"paths":["/w/1"]}`)); err == nil || !strings.Contains(err.Error(), "peer.disabled") {
		t.Fatalf("no directory = %v", err)
	}
}
