package main

import (
	"strings"
	"sync"
	"testing"
)

func testAppWithOrderedTabs(t *testing.T, active string, ids ...string) *App {
	t.Helper()
	isolateDesktopUserDirs(t)
	tabs := make(map[string]*WorkspaceTab, len(ids))
	for _, id := range ids {
		tabs[id] = &WorkspaceTab{
			ID:          id,
			Scope:       "global",
			TopicID:     "topic_" + id,
			TopicTitle:  id,
			Ready:       true,
			disabledMCP: map[string]ServerView{},
		}
	}
	return &App{tabs: tabs, tabOrder: append([]string(nil), ids...), activeTabID: active}
}

func tabIDs(tabs []TabMeta) []string {
	ids := make([]string, 0, len(tabs))
	for _, tab := range tabs {
		ids = append(ids, tab.ID)
	}
	return ids
}

func assertTabIDs(t *testing.T, got []TabMeta, want ...string) {
	t.Helper()
	gotIDs := tabIDs(got)
	if len(gotIDs) != len(want) {
		t.Fatalf("tab ids = %v, want %v", gotIDs, want)
	}
	for i := range want {
		if gotIDs[i] != want[i] {
			t.Fatalf("tab ids = %v, want %v", gotIDs, want)
		}
	}
}

func TestListTabsKeepsExplicitOrderWhenActiveChanges(t *testing.T) {
	app := testAppWithOrderedTabs(t, "b", "a", "b", "c")

	assertTabIDs(t, app.ListTabs(), "a", "b", "c")
	if err := app.SetActiveTab("c"); err != nil {
		t.Fatalf("SetActiveTab: %v", err)
	}
	assertTabIDs(t, app.ListTabs(), "a", "b", "c")
	if got := app.activeTabID; got != "c" {
		t.Fatalf("active tab = %q, want c", got)
	}
}

func TestListTabsRepairsStaleOrderWithoutRacing(t *testing.T) {
	app := testAppWithOrderedTabs(t, "a", "a", "b", "c")
	app.tabOrder = []string{"a"}

	var wg sync.WaitGroup
	start := make(chan struct{})
	errs := make(chan string, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for j := 0; j < 100; j++ {
				if got := strings.Join(tabIDs(app.ListTabs()), ","); got != "a,b,c" {
					errs <- got
					return
				}
			}
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for got := range errs {
		t.Fatalf("tab ids = %q, want a,b,c", got)
	}

	if got := strings.Join(app.tabOrder, ","); got != "a,b,c" {
		t.Fatalf("repaired tab order = %q, want a,b,c", got)
	}
}

func TestSaveTabsSkipsOlderSnapshot(t *testing.T) {
	app := testAppWithOrderedTabs(t, "a", "a", "b")

	app.mu.Lock()
	dir, oldEntries, oldActiveID, oldVersion := app.saveTabsCollectLocked()
	app.activeTabID = "b"
	_, newEntries, newActiveID, newVersion := app.saveTabsCollectLocked()
	app.mu.Unlock()

	app.saveTabsWrite(dir, newEntries, newActiveID, newVersion)
	app.saveTabsWrite(dir, oldEntries, oldActiveID, oldVersion)

	if got := loadTabsFile().ActiveTab; got != "b" {
		t.Fatalf("persisted active tab = %q, want b", got)
	}
}

func TestReorderTabsPersistsSubmittedOrder(t *testing.T) {
	app := testAppWithOrderedTabs(t, "a", "a", "b", "c")

	if err := app.ReorderTabs([]string{"c", "a", "b"}); err != nil {
		t.Fatalf("ReorderTabs: %v", err)
	}
	assertTabIDs(t, app.ListTabs(), "c", "a", "b")
	if got := app.activeTabID; got != "a" {
		t.Fatalf("active tab = %q, want a", got)
	}
}

func TestCloseActiveTabChoosesNeighborByOrder(t *testing.T) {
	app := testAppWithOrderedTabs(t, "b", "a", "b", "c")
	if err := app.CloseTab("b"); err != nil {
		t.Fatalf("CloseTab(b): %v", err)
	}
	assertTabIDs(t, app.ListTabs(), "a", "c")
	if got := app.activeTabID; got != "c" {
		t.Fatalf("active tab after closing middle = %q, want c", got)
	}

	if err := app.CloseTab("c"); err != nil {
		t.Fatalf("CloseTab(c): %v", err)
	}
	assertTabIDs(t, app.ListTabs(), "a")
	if got := app.activeTabID; got != "a" {
		t.Fatalf("active tab after closing last = %q, want a", got)
	}
}

func TestReorderTabsRejectsInvalidOrder(t *testing.T) {
	app := testAppWithOrderedTabs(t, "a", "a", "b", "c")
	for name, order := range map[string][]string{
		"missing":   {"a", "b"},
		"unknown":   {"a", "b", "missing"},
		"duplicate": {"a", "b", "b"},
	} {
		t.Run(name, func(t *testing.T) {
			if err := app.ReorderTabs(order); err == nil {
				t.Fatalf("ReorderTabs(%v) succeeded, want error", order)
			}
		})
	}
	assertTabIDs(t, app.ListTabs(), "a", "b", "c")
}

func TestNewUniqueTabIDLockedUsesFreshRandomID(t *testing.T) {
	app := testAppWithOrderedTabs(t, "a", "a", "b", "c")

	app.mu.Lock()
	got := app.newUniqueTabIDLocked()
	app.mu.Unlock()
	if _, exists := app.tabs[got]; exists {
		t.Fatalf("newUniqueTabIDLocked returned existing id %q", got)
	}
	if !strings.HasPrefix(got, "tab_") {
		t.Fatalf("tab id = %q, want tab_ prefix", got)
	}
	if len(got) != len("tab_")+32 {
		t.Fatalf("tab id = %q, length %d, want 36", got, len(got))
	}
}

func TestRestoredTabIDLockedReplacesEmptyAndDuplicateIDs(t *testing.T) {
	app := testAppWithOrderedTabs(t, "a", "a", "b", "c")

	app.mu.Lock()
	kept := app.restoredTabIDLocked("d")
	duplicate := app.restoredTabIDLocked("a")
	empty := app.restoredTabIDLocked(" ")
	app.mu.Unlock()

	if kept != "d" {
		t.Fatalf("restored unique id = %q, want d", kept)
	}
	for name, got := range map[string]string{"duplicate": duplicate, "empty": empty} {
		if _, exists := app.tabs[got]; exists {
			t.Fatalf("%s restored id %q already exists", name, got)
		}
		if !strings.HasPrefix(got, "tab_") {
			t.Fatalf("%s restored id = %q, want tab_ prefix", name, got)
		}
	}
}
