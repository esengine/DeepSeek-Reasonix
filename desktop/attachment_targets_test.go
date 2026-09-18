package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/control"
)

func TestAttachmentTargetRejectsReplacementBeforeFileUpload(t *testing.T) {
	root := t.TempDir()
	c := control.New(control.Options{WorkspaceRoot: root})
	t.Cleanup(c.Close)
	tab := &WorkspaceTab{ID: "a", WorkspaceRoot: root, Ctrl: c, SessionGeneration: 1}
	a := &App{tabs: map[string]*WorkspaceTab{"a": tab}, activeTabID: "a"}
	target, err := a.CaptureAttachmentTarget(ComposerTarget{Kind: "session", TabID: "a"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.ReleaseAttachmentTarget(target.Token) })
	a.mu.Lock()
	tab.SessionGeneration++
	a.mu.Unlock()
	if _, err := a.StageImageForTarget(target.Token, "paste", "shot.png", "image/png", "data:image/png;base64,"+desktopTinyPNG); err == nil {
		t.Fatal("stale target accepted")
	}
}

func TestAttachmentTargetKeepsSourceAcrossFocusSwitch(t *testing.T) {
	root := t.TempDir()
	c := control.New(control.Options{WorkspaceRoot: root})
	t.Cleanup(c.Close)
	tab := &WorkspaceTab{ID: "a", WorkspaceRoot: root, Ctrl: c, SessionGeneration: 1}
	a := &App{tabs: map[string]*WorkspaceTab{"a": tab, "b": {ID: "b", WorkspaceRoot: t.TempDir()}}, activeTabID: "a"}
	before, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	target, err := a.CaptureAttachmentTarget(ComposerTarget{Kind: "session", TabID: "a"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.ReleaseAttachmentTarget(target.Token) })
	a.attachmentIOHook = func() { a.mu.Lock(); a.activeTabID = "b"; a.mu.Unlock() }
	draft, err := a.StageImageForTarget(target.Token, "paste", "shot.png", "image/png", "data:image/png;base64,"+desktopTinyPNG)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.ReadDraftImageForTarget(target.Token, draft.DraftID); err != nil {
		t.Fatal(err)
	}
	if after, err := os.Getwd(); err != nil || before != after {
		t.Fatalf("cwd changed: %q %q %v", before, after, err)
	}
}

func TestAttachmentDraftSurvivesSameSessionRuntimeReplacement(t *testing.T) {
	root := t.TempDir()
	opts := control.Options{WorkspaceRoot: root, SessionPath: filepath.Join(root, "session.jsonl")}
	old := control.New(opts)
	t.Cleanup(old.Close)
	tab := &WorkspaceTab{ID: "a", WorkspaceRoot: root, Ctrl: old, SessionGeneration: 1}
	a := &App{tabs: map[string]*WorkspaceTab{"a": tab}, activeTabID: "a"}
	target, err := a.CaptureAttachmentTarget(ComposerTarget{Kind: "session", TabID: "a"})
	if err != nil {
		t.Fatal(err)
	}
	draft, err := a.StageImageForTarget(target.Token, "paste", "shot.png", "image/png", "data:image/png;base64,"+desktopTinyPNG)
	if err != nil {
		t.Fatal(err)
	}
	next := control.New(opts)
	t.Cleanup(next.Close)
	if err := activateReplacementController(old, next); err != nil {
		t.Fatal(err)
	}
	a.mu.Lock()
	tab.Ctrl = next
	a.mu.Unlock()
	a.ReleaseAttachmentTarget(target.Token)
	fresh, err := a.CaptureAttachmentTarget(ComposerTarget{Kind: "session", TabID: "a"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.ReleaseAttachmentTarget(fresh.Token) })
	retried, err := a.StageImageForTarget(fresh.Token, "paste", "shot.png", "image/png", "data:image/png;base64,"+desktopTinyPNG)
	if err != nil || retried.DraftID != draft.DraftID {
		t.Fatalf("stage receipt did not survive replacement: %+v %v", retried, err)
	}
	rebound, err := a.RebindDraftImageForTarget(fresh.Token, draft.DraftID)
	if err != nil {
		t.Fatal(err)
	}
	if rebound.DraftID == draft.DraftID {
		t.Fatal("runtime replacement did not renew draft credential")
	}
	again, err := a.RebindDraftImageForTarget(fresh.Token, draft.DraftID)
	if err != nil || again.DraftID != rebound.DraftID {
		t.Fatalf("rebind retry changed credential: %+v %v", again, err)
	}
	if next.RuntimeStatus().Running {
		t.Fatal("rebind automatically started a turn")
	}
}

func TestStageImageForTargetIsIdempotentAndRejectsConflicts(t *testing.T) {
	root := t.TempDir()
	c := control.New(control.Options{WorkspaceRoot: root, SessionPath: filepath.Join(root, "session.jsonl")})
	t.Cleanup(c.Close)
	tab := &WorkspaceTab{ID: "a", WorkspaceRoot: root, Ctrl: c, SessionGeneration: 1}
	a := &App{tabs: map[string]*WorkspaceTab{"a": tab}, activeTabID: "a"}
	target, err := a.CaptureAttachmentTarget(ComposerTarget{Kind: "session", TabID: "a"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.ReleaseAttachmentTarget(target.Token) })
	first, err := a.StageImageForTarget(target.Token, "same-operation", "shot.png", "image/png", "data:image/png;base64,"+desktopTinyPNG)
	if err != nil {
		t.Fatal(err)
	}
	retry, err := a.StageImageForTarget(target.Token, "same-operation", "shot.png", "image/png", "data:image/png;base64,"+desktopTinyPNG)
	if err != nil || retry != first {
		t.Fatalf("retry = %+v, %v; want %+v", retry, err, first)
	}
	if _, err := a.StageImageForTarget(target.Token, "same-operation", "different.png", "image/png", "data:image/png;base64,"+desktopTinyPNG); err == nil || !strings.Contains(err.Error(), "conflicts") {
		t.Fatalf("different input conflict = %v", err)
	}
	if err := a.ReleaseDraftImageForTarget(target.Token, first.DraftID); err != nil {
		t.Fatal(err)
	}
	afterRelease, err := a.StageImageForTarget(target.Token, "same-operation", "shot.png", "image/png", "data:image/png;base64,"+desktopTinyPNG)
	if err != nil {
		t.Fatal(err)
	}
	if afterRelease.DraftID == first.DraftID {
		t.Fatal("released credential retained its staging receipt")
	}
}

func TestStageImageForTargetConcurrentRetrySharesOneOperation(t *testing.T) {
	root := t.TempDir()
	c := control.New(control.Options{WorkspaceRoot: root, SessionPath: filepath.Join(root, "session.jsonl")})
	t.Cleanup(c.Close)
	tab := &WorkspaceTab{ID: "a", WorkspaceRoot: root, Ctrl: c, SessionGeneration: 1}
	a := &App{tabs: map[string]*WorkspaceTab{"a": tab}, activeTabID: "a"}
	target, err := a.CaptureAttachmentTarget(ComposerTarget{Kind: "session", TabID: "a"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.ReleaseAttachmentTarget(target.Token) })

	ioEntered := make(chan struct{})
	joinEntered := make(chan struct{})
	releaseIO := make(chan struct{})
	var ioOnce, joinOnce sync.Once
	a.attachmentIOHook = func() {
		ioOnce.Do(func() { close(ioEntered) })
		<-releaseIO
	}
	a.attachmentJoinHook = func() { joinOnce.Do(func() { close(joinEntered) }) }
	type result struct {
		view DraftImageView
		err  error
	}
	firstDone := make(chan result, 1)
	secondDone := make(chan result, 1)
	go func() {
		view, err := a.StageImageForTarget(target.Token, "concurrent", "shot.png", "image/png", "data:image/png;base64,"+desktopTinyPNG)
		firstDone <- result{view: view, err: err}
	}()
	select {
	case <-ioEntered:
	case <-time.After(5 * time.Second):
		t.Fatal("first stage did not reach the I/O barrier")
	}
	go func() {
		view, err := a.StageImageForTarget(target.Token, "concurrent", "shot.png", "image/png", "data:image/png;base64,"+desktopTinyPNG)
		secondDone <- result{view: view, err: err}
	}()
	select {
	case <-joinEntered:
	case <-time.After(5 * time.Second):
		t.Fatal("retry did not join the in-flight operation")
	}
	close(releaseIO)
	first, second := <-firstDone, <-secondDone
	if first.err != nil || second.err != nil || first.view != second.view {
		t.Fatalf("concurrent results = %+v / %+v", first, second)
	}
}

func TestStageImageForTargetBoundsOwnerOperations(t *testing.T) {
	root := t.TempDir()
	c := control.New(control.Options{WorkspaceRoot: root, SessionPath: filepath.Join(root, "session.jsonl")})
	t.Cleanup(c.Close)
	tab := &WorkspaceTab{ID: "a", WorkspaceRoot: root, Ctrl: c, SessionGeneration: 1}
	a := &App{tabs: map[string]*WorkspaceTab{"a": tab}, activeTabID: "a"}
	target, err := a.CaptureAttachmentTarget(ComposerTarget{Kind: "session", TabID: "a"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.ReleaseAttachmentTarget(target.Token) })
	for i := range 256 {
		if _, err := a.StageImageForTarget(target.Token, fmt.Sprintf("operation-%d", i), "shot.png", "image/png", "data:image/png;base64,"+desktopTinyPNG); err != nil {
			t.Fatalf("operation %d: %v", i, err)
		}
	}
	if _, err := a.StageImageForTarget(target.Token, "operation-overflow", "shot.png", "image/png", "data:image/png;base64,"+desktopTinyPNG); err == nil || !strings.Contains(err.Error(), "too many") {
		t.Fatalf("overflow = %v", err)
	}
}

func TestAttachmentTargetCancellationIsIndependent(t *testing.T) {
	c := control.New(control.Options{WorkspaceRoot: t.TempDir()})
	t.Cleanup(c.Close)
	a := &App{tabs: map[string]*WorkspaceTab{"a": {ID: "a", WorkspaceRoot: t.TempDir(), Ctrl: c}}, activeTabID: "a"}
	one, err := a.CaptureAttachmentTarget(ComposerTarget{Kind: "session", TabID: "a"})
	if err != nil {
		t.Fatal(err)
	}
	two, err := a.CaptureAttachmentTarget(ComposerTarget{Kind: "session", TabID: "a"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.ReleaseAttachmentTarget(two.Token) })
	a.ReleaseAttachmentTarget(one.Token)
	if _, err := a.StageImageForTarget(one.Token, "one", "a.png", "image/png", "data:image/png;base64,"+desktopTinyPNG); err == nil {
		t.Fatal("canceled target accepted")
	}
	if _, err := a.StageImageForTarget(two.Token, "two", "a.png", "image/png", "data:image/png;base64,"+desktopTinyPNG); err != nil {
		t.Fatal(err)
	}
}
