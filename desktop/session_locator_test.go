package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/store"
)

func TestSessionLocatorSeparatesCanonicalRoutesFromLegacyPaths(t *testing.T) {
	legacy := filepath.Join(t.TempDir(), "session-id:real.jsonl")
	tests := []struct {
		name string
		raw  string
		kind sessionLocatorKind
		id   string
	}{
		{name: "empty", raw: "  ", kind: sessionLocatorEmpty},
		{name: "canonical", raw: " session-id:valid-id ", kind: sessionLocatorCanonical, id: "valid-id"},
		{name: "empty canonical id", raw: "session-id:", kind: sessionLocatorInvalid},
		{name: "canonical separator", raw: "session-id:bad/id", kind: sessionLocatorInvalid},
		{name: "canonical reserved name", raw: "session-id:CON", kind: sessionLocatorInvalid},
		{name: "canonical control", raw: "session-id:bad\x01id", kind: sessionLocatorInvalid},
		{name: "canonical windows punctuation", raw: `session-id:bad<id>`, kind: sessionLocatorInvalid},
		{name: "canonical question mark", raw: `session-id:bad?id`, kind: sessionLocatorInvalid},
		{name: "canonical too long", raw: "session-id:" + strings.Repeat("x", 256), kind: sessionLocatorInvalid},
		{name: "legacy colon filename", raw: legacy, kind: sessionLocatorLegacy},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := classifySessionLocator(test.raw)
			if got.kind != test.kind || got.ref.SessionID != test.id {
				t.Fatalf("locator = kind:%v id:%q reason:%q", got.kind, got.ref.SessionID, got.reason)
			}
		})
	}
}

func TestSavedTabRouteCandidateUsesOnlyExactRouteComponents(t *testing.T) {
	tests := []struct {
		name, raw, kind, id string
	}{
		{name: "exact", raw: "session-id:valid-id", kind: "canonical_route", id: "valid-id"},
		{name: "windows drive", raw: `C:\old\session-id:valid-id`, kind: "pseudo_route_path", id: "valid-id"},
		{name: "windows UNC", raw: `\\server\share\session-id:valid-id`, kind: "pseudo_route_path", id: "valid-id"},
		{name: "posix", raw: "/old/session-id:valid-id", kind: "pseudo_route_path", id: "valid-id"},
		{name: "relative", raw: "old/session-id:valid-id"},
		{name: "intermediate component", raw: "/old/session-id:valid-id/history"},
		{name: "transcript", raw: "/old/session-id:valid-id.jsonl"},
		{name: "sidecar", raw: "/old/session-id:valid-id.jsonl.meta"},
		{name: "invalid exact", raw: "session-id:bad/id", kind: "invalid_route"},
		{name: "invalid pseudo", raw: "/old/session-id:bad?id", kind: "invalid_route"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := savedTabRouteCandidateForPath(test.raw)
			if got.kind != test.kind || got.sessionID != test.id {
				t.Fatalf("candidate = kind:%q id:%q", got.kind, got.sessionID)
			}
		})
	}
}

func TestTabSessionMetaRejectsRouteBeforeCreatingSidecars(t *testing.T) {
	isolateDesktopUserDirs(t)
	app := NewApp()
	t.Cleanup(func() { _ = app.draftStore().Close() })
	tab := &WorkspaceTab{ID: "route", Scope: "global", SessionPath: "session-id:valid-id"}
	app.tabs[tab.ID] = tab

	if err := app.saveTabSessionMetaForCurrentSession(tab); err != nil {
		t.Fatalf("canonical compatibility route should skip legacy metadata: %v", err)
	}
	if _, err := os.Stat(store.SessionMeta(filepath.Join(desktopSessionDir(""), "session-id:valid-id"))); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("canonical route produced a metadata sidecar: %v", err)
	}

	tab.SessionPath = "session-id:bad/id"
	if err := app.saveTabSessionMetaForCurrentSession(tab); err == nil {
		t.Fatal("invalid canonical route was silently accepted")
	}
}

func TestCanonicalTabSessionMetaNeverWritesLegacySidecar(t *testing.T) {
	isolateDesktopUserDirs(t)
	app := NewApp()
	t.Cleanup(func() { _ = app.draftStore().Close() })
	tab := &WorkspaceTab{ID: "canonical", Scope: "global", SessionID: "canonical-id", SessionPath: "session-id:canonical-id"}
	app.tabs[tab.ID] = tab

	if err := app.saveTabSessionMetaForCurrentSession(tab); err != nil {
		t.Fatal(err)
	}
	if err := app.saveTabSessionMeta(tab, "session-id:canonical-id"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat("session-id:canonical-id.meta"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("canonical tab wrote legacy metadata: %v", err)
	}
}

func TestSessionLocatorNeverCanonicalizesRouteAsPath(t *testing.T) {
	if got := canonicalTabSessionPath("session-id:valid-id"); got != "" {
		t.Fatalf("canonical route became path %q", got)
	}
	if got := canonicalTabSessionPath("session-id:bad/id"); got != "" {
		t.Fatalf("invalid canonical route became path %q", got)
	}
	if got := sessionRuntimeKey("session-id:valid-id"); got != "session-id:valid-id" {
		t.Fatalf("canonical runtime key = %q", got)
	}
	if got := sessionRuntimeKey("session-id:bad/id"); got != "" {
		t.Fatalf("invalid route runtime key = %q", got)
	}
	if got := sessionRuntimeKey(filepath.Join(t.TempDir(), "not-a-transcript.meta")); got != "" {
		t.Fatalf("invalid legacy runtime key = %q", got)
	}
}

func TestResolveLegacySessionPathRejectsRoutesBeforePathResolution(t *testing.T) {
	dir := t.TempDir()
	if _, err := resolveLegacySessionPath("session-id:valid-id", dir); err == nil {
		t.Fatal("canonical route was accepted as a legacy path")
	}
	if _, err := resolveLegacySessionPath("session-id:bad/id", dir); err == nil {
		t.Fatal("invalid route was accepted as a legacy path")
	}
	legacy := filepath.Join(dir, "history.jsonl")
	got, err := resolveLegacySessionPath(legacy, dir)
	if err != nil || string(got) != legacy {
		t.Fatalf("legacy path = %q, %v", got, err)
	}
}

func TestLegacyReadersRejectCanonicalAndInvalidRoutes(t *testing.T) {
	for _, route := range []string{"session-id:valid-id", "session-id:bad/id"} {
		if _, ok := validatedLegacySessionPathForRead(route); ok {
			t.Fatalf("route %q was accepted for legacy reads", route)
		}
		if profile := loadTabSessionProfile(route); profile != defaultTabSessionProfile() {
			t.Fatalf("route %q loaded a legacy profile: %+v", route, profile)
		}
		if got := runningTabSessionGoal(route, "persisted goal"); got != "persisted goal" {
			t.Fatalf("route %q read a legacy goal: %q", route, got)
		}
		if _, _, ok := topicTitleFallbackForOpen("", "topic", route); ok {
			t.Fatalf("route %q loaded a legacy title", route)
		}
	}
}

func TestSessionLeaseRejectsRouteBeforeFileAccess(t *testing.T) {
	tab := &WorkspaceTab{}
	if err := tab.ensureSessionLease("session-id:valid-id"); err != nil {
		t.Fatalf("canonical route should use Session Service ownership: %v", err)
	}
	if tab.sessionLease != nil {
		t.Fatal("canonical route acquired a legacy file lease")
	}
	if err := tab.ensureSessionLease("session-id:bad/id"); err == nil {
		t.Fatal("invalid route was silently accepted by the lease boundary")
	}
	for _, artifact := range []string{"session-id:valid-id.lease.lock", "session-id:valid-id.lease.json"} {
		if _, err := os.Lstat(artifact); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("canonical route created %q: %v", artifact, err)
		}
	}
}

func TestCanonicalHiddenTabPruneSkipsLegacyMetadataWriter(t *testing.T) {
	app := newSavedTabReconcileTestApp(t)
	root := t.TempDir()
	ref, workspaceID := createLegacyCleanupSession(t, app, root, "hidden-canonical", true)
	tab := &WorkspaceTab{
		ID: "hidden", Scope: "project", WorkspaceRoot: root, SessionWorkspace: desktopTabWorkspace{ID: workspaceID},
		SessionID: ref.SessionID, SessionPath: sessionRoute(ref.SessionID),
	}
	app.tabs[tab.ID] = tab
	if err := app.persistHiddenTabBeforePrune(tab.ID, tab); err != nil {
		t.Fatal(err)
	}
	for _, artifact := range []string{"session-id:hidden-canonical.meta", "session-id:hidden-canonical.lock"} {
		if _, err := os.Lstat(artifact); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("canonical hidden-tab prune created %q: %v", artifact, err)
		}
	}
}

func TestCanonicalDetachCloneAndReattachKeepExclusiveIdentity(t *testing.T) {
	source := &WorkspaceTab{ID: "source", Scope: "global", SessionID: "canonical-transfer", SessionPath: sessionRoute("canonical-transfer")}
	detached := cloneDetachedRuntimeTab(source, sessionRoute("canonical-transfer"), source.currentSessionIdentity())
	if detached == nil || detached.SessionID != "canonical-transfer" || detached.SessionPath != "" {
		t.Fatalf("detached identity = id:%q path:%q", detached.SessionID, detached.SessionPath)
	}
	target := &WorkspaceTab{ID: "target", Scope: "global"}
	applyRuntimeTab(target, detached, sessionRoute("canonical-transfer"), context.Background(), nil)
	if target.SessionID != "canonical-transfer" || target.SessionPath != "" {
		t.Fatalf("reattached identity = id:%q path:%q", target.SessionID, target.SessionPath)
	}
}
