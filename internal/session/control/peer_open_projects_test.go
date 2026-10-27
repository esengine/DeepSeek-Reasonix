package control

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

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
	a1, a2 := filepath.Join(t.TempDir(), "a"), filepath.Join(t.TempDir(), "b")
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
	done := openProjectsAsync(c, filepath.Join(t.TempDir(), "a"))
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
		many = append(many, filepath.Join(t.TempDir(), string(rune('a'+i))))
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
	if _, err := c.OpenProjects(context.Background(), []string{filepath.Join(t.TempDir(), "a")}); !errors.Is(err, ErrDispatchDisabled) {
		t.Fatalf("switch off = %v", err)
	}
	setDispatchSwitch(t, true, true)
	c2 := c
	if _, err := c2.OpenProjects(context.Background(), []string{filepath.Join(t.TempDir(), "a")}); !errors.Is(err, ErrDispatchUnavailable) {
		t.Fatalf("no host = %v", err)
	}
	if got := peerErrorCode(ErrOpenProjectsDeclined); got != "open_projects.declined" {
		t.Fatalf("code = %q", got)
	}
}
