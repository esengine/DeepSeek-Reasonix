package control

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/tool"
)

type opened struct {
	got []tool.OpenedProject
	err error
}

func openProjectsAsync(c *Controller, paths ...string) chan opened {
	done := make(chan opened, 1)
	go func() {
		got, err := c.OpenProjects(context.Background(), paths)
		done <- opened{got, err}
	}()
	return done
}

func TestOpeningProjectsWaitsForAPersonEvenInYoloAndActsOnExactlyTheCardsList(t *testing.T) {
	c, requests, _ := settingController(t)
	setDispatchSwitch(t, true, true)
	var seen [][]string
	c.SetProjectOpener(func(_ context.Context, _ *Controller, paths []string) ([]tool.OpenedProject, error) {
		seen = append(seen, paths)
		return []tool.OpenedProject{{Path: paths[0], Added: true}}, nil
	})
	c.SetAutoApproveTools(true)
	a1, a2 := filepath.Join(testenv.TempDir(t), "a"), filepath.Join(testenv.TempDir(t), "b")
	done := openProjectsAsync(c, a1+string(filepath.Separator), a2, a1)
	a := nextApproval(t, requests)
	if a.Tool != OpenProjectsApprovalTool || !a.Fresh || a.AllowsSession || a.AllowsPersist {
		t.Fatalf("approval = %+v, want a fresh card with no grant to remember", a)
	}
	select {
	case got := <-done:
		t.Fatalf("answered without a person: %+v", got)
	case <-time.After(50 * time.Millisecond):
	}
	if len(seen) != 0 {
		t.Fatal("the host was asked before the click")
	}
	c.Approve(a.ID, true, false, false)
	got := <-done
	if got.err != nil || len(seen) != 1 || len(seen[0]) != 2 || seen[0][0] != a1 || seen[0][1] != a2 {
		t.Fatalf("host saw %v, result %+v err=%v; want the cleaned, de-duplicated list the card showed", seen, got.got, got.err)
	}
}

func TestADeclinedOpenProjectsCardAddsNothing(t *testing.T) {
	c, requests, _ := settingController(t)
	setDispatchSwitch(t, true, true)
	called := false
	c.SetProjectOpener(func(context.Context, *Controller, []string) ([]tool.OpenedProject, error) {
		called = true
		return nil, nil
	})
	done := openProjectsAsync(c, filepath.Join(testenv.TempDir(t), "a"))
	a := nextApproval(t, requests)
	c.Approve(a.ID, false, false, false)
	got := <-done
	if !errors.Is(got.err, ErrOpenProjectsDeclined) || called {
		t.Fatalf("err = %v called = %v, want declined and no call", got.err, called)
	}
}

func TestOpenProjectsRefusesBeforeAnyCardWhatCannotBeAsked(t *testing.T) {
	c, requests, _ := settingController(t)
	setDispatchSwitch(t, true, true)
	c.SetProjectOpener(func(context.Context, *Controller, []string) ([]tool.OpenedProject, error) { return nil, nil })
	var many []string
	for i := range openProjectsMax + 1 {
		many = append(many, filepath.Join(testenv.TempDir(t), string(rune('a'+i))))
	}
	for name, tc := range map[string]struct {
		paths []string
		want  error
	}{
		"none":     {nil, ErrOpenProjectsEmpty},
		"blank":    {[]string{"  "}, ErrOpenProjectsEmpty},
		"relative": {[]string{"sub/dir"}, ErrOpenProjectsRelative},
		"too many": {many, ErrOpenProjectsTooMany},
	} {
		if _, err := c.OpenProjects(context.Background(), tc.paths); !errors.Is(err, tc.want) {
			t.Errorf("%s: err = %v, want %v", name, err, tc.want)
		}
	}
	select {
	case a := <-requests:
		t.Fatalf("a card was raised for a call that could not be asked: %+v", a)
	default:
	}
}

func TestOpenProjectsNeedsTheDispatchSwitchAndAHost(t *testing.T) {
	c, _, _ := settingController(t)
	setDispatchSwitch(t, true, false)
	if _, err := c.OpenProjects(context.Background(), []string{filepath.Join(testenv.TempDir(t), "a")}); !errors.Is(err, ErrDispatchDisabled) {
		t.Fatalf("switch off = %v", err)
	}
	setDispatchSwitch(t, true, true)
	c2 := c
	if _, err := c2.OpenProjects(context.Background(), []string{filepath.Join(testenv.TempDir(t), "a")}); !errors.Is(err, ErrDispatchUnavailable) {
		t.Fatalf("no host = %v", err)
	}
	if got := peerErrorCode(ErrOpenProjectsDeclined); got != "open_projects.declined" {
		t.Fatalf("code = %q", got)
	}
}

func openNestedController(t *testing.T, root string, on bool) (*Controller, chan event.Approval, *[][]string) {
	t.Helper()
	setDispatchSwitch(t, true, true)
	cfg := config.LoadForEdit(config.UserConfigPath())
	cfg.Peers.OpenNested = on
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatal(err)
	}
	requests := make(chan event.Approval, 8)
	c := New(Options{WorkspaceRoot: root, Sink: event.FuncSink(func(e event.Event) {
		if e.Kind == event.ApprovalRequest {
			requests <- e.Approval
		}
	})})
	var seen [][]string
	c.SetProjectOpener(func(_ context.Context, _ *Controller, paths []string) ([]tool.OpenedProject, error) {
		seen = append(seen, paths)
		out := make([]tool.OpenedProject, len(paths))
		for i, p := range paths {
			out[i] = tool.OpenedProject{Path: p, Added: true}
		}
		return out, nil
	})
	return c, requests, &seen
}

func TestFoldersInsideTheWorkspaceOpenWithoutACardWhenTheUserAllowedIt(t *testing.T) {
	root := testenv.TempDir(t)
	inner := filepath.Join(root, "src", "systems")
	other := filepath.Join(root, ".agent")
	for _, d := range []string{inner, other} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	c, requests, seen := openNestedController(t, root, true)
	done := openProjectsAsync(c, inner, other)
	select {
	case a := <-requests:
		t.Fatalf("a card was shown for folders inside the workspace: %+v", a)
	case got := <-done:
		if got.err != nil || len(*seen) != 1 || len((*seen)[0]) != 2 || len(got.got) != 2 {
			t.Fatalf("got %+v err=%v host saw %v, want both folders opened by the host", got.got, got.err, *seen)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("OpenProjects neither opened the folders nor asked")
	}
}

func TestALinkInsideTheWorkspaceThatLeavesItStillAsks(t *testing.T) {
	root, outside := testenv.TempDir(t), testenv.TempDir(t)
	link := filepath.Join(root, "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("cannot make a symlink here: %v", err)
	}
	c, requests, seen := openNestedController(t, root, true)
	done := openProjectsAsync(c, link)
	a := nextApproval(t, requests)
	if a.Tool != OpenProjectsApprovalTool || len(*seen) != 0 {
		t.Fatalf("approval = %+v seen=%v, want a card and no host call", a, *seen)
	}
	c.Approve(a.ID, false, false, false)
	<-done
}

func TestAFolderOutsideTheWorkspaceStillAsksEvenWhenTheOthersAreInside(t *testing.T) {
	root, outside := testenv.TempDir(t), testenv.TempDir(t)
	inner := filepath.Join(root, "pkg")
	if err := os.MkdirAll(inner, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, paths := range map[string][]string{
		"one outside":       {inner, outside},
		"parent traversal":  {filepath.Join(root, "..", filepath.Base(outside))},
		"the root's parent": {filepath.Dir(root)},
	} {
		t.Run(name, func(t *testing.T) {
			c, requests, seen := openNestedController(t, root, true)
			done := openProjectsAsync(c, paths...)
			a := nextApproval(t, requests)
			if a.Tool != OpenProjectsApprovalTool || !a.Fresh {
				t.Fatalf("approval = %+v, want the fresh open_projects card", a)
			}
			if len(*seen) != 0 {
				t.Fatal("the host was asked before the click")
			}
			c.Approve(a.ID, false, false, false)
			<-done
		})
	}
}

func TestInsideTheWorkspaceStillAsksWhenTheUserHasNotAllowedIt(t *testing.T) {
	root := testenv.TempDir(t)
	inner := filepath.Join(root, "pkg")
	if err := os.MkdirAll(inner, 0o755); err != nil {
		t.Fatal(err)
	}
	c, requests, seen := openNestedController(t, root, false)
	done := openProjectsAsync(c, inner)
	a := nextApproval(t, requests)
	if a.Tool != OpenProjectsApprovalTool || len(*seen) != 0 {
		t.Fatalf("approval = %+v seen=%v, want a card and no host call", a, *seen)
	}
	c.Approve(a.ID, false, false, false)
	<-done
}

func TestAFolderThatDoesNotExistIsNotInsideAnything(t *testing.T) {
	root := testenv.TempDir(t)
	c, requests, _ := openNestedController(t, root, true)
	done := openProjectsAsync(c, filepath.Join(root, "missing"))
	a := nextApproval(t, requests)
	c.Approve(a.ID, false, false, false)
	<-done
}
