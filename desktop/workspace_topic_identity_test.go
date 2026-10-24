package main

import (
	"path/filepath"
	"slices"
	"testing"

	"reasonix/desktop/internal/workspacestate"
)

func TestWorkspaceSourceAliasesKeepReviewedSessionsIndependent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	key := desktopSourceKey(path, "")
	state := workspacestate.State{
		Workspaces: map[string]workspacestate.Workspace{"w": {ID: "w", SessionIDs: []string{"original", "reviewed"}}},
		SourceMappings: map[string]workspacestate.SourceMapping{
			key:                {SourceKey: key, Path: path, WorkspaceID: "w", SessionID: "original"},
			key + ":review:v2": {SourceKey: key + ":review:v2", Path: path, WorkspaceID: "w", SessionID: "reviewed"},
		},
	}
	aliases := workspaceSourceAliases(state, "w")
	if !slices.Contains(aliases["original"], "source\x00local\x00"+key) {
		t.Fatal("original source must still resolve to its canonical owner")
	}
	if !slices.Contains(aliases["reviewed"], "source\x00local\x00"+key+":review:v2") {
		t.Fatal("reviewed source must retain its versioned identity")
	}
	for _, alias := range aliases["original"] {
		if slices.Contains(aliases["reviewed"], alias) {
			t.Fatalf("independent sessions share a sidebar identity: %q", alias)
		}
	}
}

func TestWorkspaceSourceAliasesRejectAmbiguousPathOwnership(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	state := workspacestate.State{SourceMappings: map[string]workspacestate.SourceMapping{
		"old-spelling": {SourceKey: "old-spelling", Path: path, WorkspaceID: "w", SessionID: "first"},
		"new-spelling": {SourceKey: "new-spelling", Path: path, WorkspaceID: "w", SessionID: "second"},
	}}
	aliases := workspaceSourceAliases(state, "w")
	for _, alias := range aliases["first"] {
		if slices.Contains(aliases["second"], alias) {
			t.Fatalf("ambiguous normalized source was assigned to two sessions: %q", alias)
		}
	}
	if !slices.Contains(aliases["first"], "source\x00local\x00old-spelling") || !slices.Contains(aliases["second"], "source\x00local\x00new-spelling") {
		t.Fatal("exact durable receipts must remain independently addressable")
	}
}

func TestWorkspacePlaceholderRuntimeIdentity(t *testing.T) {
	isolateDesktopUserDirs(t)
	a := NewApp()
	root := t.TempDir()
	nodes := a.projectTreeRuntimeTopics([]catalogRuntimeSnapshot{{
		tabID: "starting", scope: "project", workspaceRoot: root, topicID: "draft", open: true,
	}})
	if len(nodes) != 1 || !slices.Contains(nodes[0].Node.IdentityAliases, "topic\x00draft") {
		t.Fatalf("startup must identify its owned placeholder: %+v", nodes)
	}
}

func TestWorkspaceCanonicalPlaceholderAliasIsUnambiguous(t *testing.T) {
	state := workspacestate.State{
		Workspaces:   map[string]workspacestate.Workspace{"w": {ID: "w", SessionIDs: []string{"first"}}},
		Presentation: map[string]workspacestate.Presentation{"first": {TopicID: "draft"}, "branch": {TopicID: "draft"}},
	}
	if !slices.Contains(sourceAliases(state, "w", "first"), "topic\x00draft") {
		t.Fatal("the first durable session must retire its placeholder")
	}
	w := state.Workspaces["w"]
	w.SessionIDs = append(w.SessionIDs, "branch")
	state.Workspaces["w"] = w
	for _, id := range w.SessionIDs {
		if slices.Contains(sourceAliases(state, "w", id), "topic\x00draft") {
			t.Fatal("a shared topic is not a unique session alias")
		}
	}
}
