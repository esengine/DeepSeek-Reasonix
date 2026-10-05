package control

import (
	"context"
	"errors"
	"testing"

	"reasonix/internal/platform/browser"
)

type fakeTabs struct {
	tabs   []browser.TabInfo
	active string
	fail   error
}

func (f *fakeTabs) Tabs() []browser.TabInfo {
	out := make([]browser.TabInfo, len(f.tabs))
	for i, t := range f.tabs {
		t.Active = t.ID == f.active
		out[i] = t
	}
	return out
}

func (f *fakeTabs) Visit(_ context.Context, rawURL, _ string, _ bool) (browser.TabInfo, error) {
	t := browser.TabInfo{ID: "t" + string(rune('1'+len(f.tabs))), URL: rawURL, Active: true}
	f.tabs = append(f.tabs, t)
	f.active = t.ID
	return t, f.fail
}

func (f *fakeTabs) Switch(id string) (browser.TabInfo, error) {
	f.active = id
	return browser.TabInfo{ID: id, Active: true}, nil
}

// The person opening a tab must not move where the agent is looking: its next
// call without a tab would otherwise act on a page it never opened.
func TestOpenBesideKeepsTheAgentsTabActive(t *testing.T) {
	f := &fakeTabs{tabs: []browser.TabInfo{{ID: "t1", URL: "https://top.baidu.com"}}, active: "t1"}
	got, err := openBeside(context.Background(), f, "about:blank")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "t2" || got.Active {
		t.Fatalf("opened = %+v, want t2 not active", got)
	}
	if f.active != "t1" {
		t.Fatalf("active = %q, want the agent's t1", f.active)
	}
}

func TestOpenBesideWithNoTabLeavesTheNewOneActive(t *testing.T) {
	f := &fakeTabs{}
	got, err := openBeside(context.Background(), f, "https://example.com")
	if err != nil || !got.Active || f.active != got.ID {
		t.Fatalf("opened = %+v, err %v, active %q; want the only tab active", got, err, f.active)
	}
}

func TestOpenBesideHandsBackEvenWhenTheLoadFails(t *testing.T) {
	f := &fakeTabs{tabs: []browser.TabInfo{{ID: "t1"}}, active: "t1", fail: errors.New("navigation timeout")}
	if _, err := openBeside(context.Background(), f, "https://slow.example"); err == nil {
		t.Fatal("the load failure was swallowed")
	}
	if f.active != "t1" {
		t.Fatalf("active = %q after a failed load, want t1", f.active)
	}
}
