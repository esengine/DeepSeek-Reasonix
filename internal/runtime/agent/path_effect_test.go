package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reasonix/internal/runtime/writeclaim"
	"reasonix/internal/state/checkpoint"
	"runtime"
	"slices"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/safety/evidence"
)

func TestObservationNamesWhatAnOpaqueCommandTouched(t *testing.T) {
	dir := testenv.TempDir(t)
	edited := filepath.Join(dir, "parse.go")
	scratch := filepath.Join(dir, "probe.go")
	if err := os.WriteFile(edited, []byte("before\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	ledger := evidence.NewLedger()
	ledger.Record(evidence.Receipt{ToolName: "read_file", Success: true, Read: true, Paths: []string{edited}})
	plan := &toolCallPlan{pathsBefore: snapshotPaths(ledger, dir, []string{scratch})}

	// What `sed -i parse.go` and a scratch file would do, without the host
	// knowing either command.
	if err := os.WriteFile(edited, []byte("after\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.WriteFile(scratch, []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	rec := evidence.Receipt{ToolName: "bash", Success: true, Mutation: true, MutationEvidence: evidence.MutationUnknown}
	decorateObservedPaths(&rec, plan)

	if rec.MutationEvidence != evidence.MutationProven {
		t.Fatalf("MutationEvidence = %q, want proven", rec.MutationEvidence)
	}
	if len(rec.Paths) != 2 {
		t.Fatalf("Paths = %v, want both touched files", rec.Paths)
	}
	if len(rec.Created) != 1 || rec.Created[0] != scratch {
		t.Fatalf("Created = %v, want only the new file", rec.Created)
	}
}

// A call that changed nothing among the watched paths may still have changed
// something outside them, so it must not be downgraded to "changed nothing".
func TestObservationNeverDowngradesAnUnknownCall(t *testing.T) {
	dir := testenv.TempDir(t)
	quiet := filepath.Join(dir, "untouched.go")
	if err := os.WriteFile(quiet, []byte("x\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	ledger := evidence.NewLedger()
	ledger.Record(evidence.Receipt{ToolName: "read_file", Success: true, Read: true, Paths: []string{quiet}})

	rec := evidence.Receipt{ToolName: "bash", Success: true, Mutation: true, MutationEvidence: evidence.MutationUnknown}
	decorateObservedPaths(&rec, &toolCallPlan{pathsBefore: snapshotPaths(ledger, dir, nil)})

	if rec.MutationEvidence != evidence.MutationUnknown {
		t.Fatalf("MutationEvidence = %q, want it left unknown", rec.MutationEvidence)
	}
}

// The investigation shape: a scratch file written, used, and removed leaves
// nothing to verify, while an edit that is still on disk does.
func TestBaselineIgnoresScratchFilesTheTurnCleanedUp(t *testing.T) {
	dir := testenv.TempDir(t)
	scratch := filepath.Join(dir, "zz_probe.go")
	kept := filepath.Join(dir, "stats.go")

	ledger := evidence.NewLedger()
	cleaned := evidence.Receipt{
		ToolName: "write_file", Success: true, Write: true, Mutation: true,
		MutationEvidence: evidence.MutationProven,
		Paths:            []string{scratch},
		Created:          []string{scratch},
	}
	ledger.Record(cleaned)
	if leftSomethingBehind(ledger, cleaned) {
		t.Fatal("a created file that is now gone left nothing to verify")
	}

	if err := os.WriteFile(kept, []byte("package logstat\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	survivor := cleaned
	survivor.Paths = []string{kept}
	survivor.Created = []string{kept}
	ledger.Record(survivor)
	if !leftSomethingBehind(ledger, survivor) {
		t.Fatal("a created file still on disk must be verified")
	}
}

// An edit to a file that already existed is never "cleaned up", even if the
// path later becomes unreadable: the host only vouches for what it watched
// appear.
func TestBaselineKeepsWritesItDidNotWatchCreate(t *testing.T) {
	gone := filepath.Join(testenv.TempDir(t), "never-existed.go")
	edit := evidence.Receipt{
		ToolName: "edit_file", Success: true, Write: true, Mutation: true,
		MutationEvidence: evidence.MutationProven,
		Paths:            []string{gone},
	}
	if !leftSomethingBehind(evidence.NewLedger(), edit) {
		t.Fatal("a write with no observed creation must count as surviving")
	}
}

func TestObservedPathsSurviveReceiptRoundTrip(t *testing.T) {
	rec := evidence.Receipt{ToolName: "bash", Created: []string{"a.go"}}
	raw, err := json.Marshal(rec)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back evidence.Receipt
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(back.Created) != 1 || back.Created[0] != "a.go" {
		t.Fatalf("Created = %v after round trip", back.Created)
	}
}

// The build-artifact shape: a binary the workspace never named appears, is
// used, and is removed. Watching the top level is what lets the host see both
// halves, so the cleanup does not read as an unverified change.
func TestBuildArtifactCreatedAndRemovedLeavesNothingBehind(t *testing.T) {
	dir := testenv.TempDir(t)
	artifact := filepath.Join(dir, "tally-bin")
	ledger := evidence.NewLedger()

	build := &toolCallPlan{pathsBefore: snapshotPaths(ledger, dir, nil)}
	if err := os.WriteFile(artifact, []byte("binary\n"), 0o755); err != nil {
		t.Fatalf("write: %v", err)
	}
	buildRec := evidence.Receipt{ToolName: "bash", Success: true, Mutation: true, MutationEvidence: evidence.MutationUnknown}
	decorateObservedPaths(&buildRec, build)
	ledger.Record(buildRec)
	if !slices.Contains(buildRec.Created, artifact) {
		t.Fatalf("Created = %v, want the artifact the build produced", buildRec.Created)
	}

	cleanup := &toolCallPlan{pathsBefore: snapshotPaths(ledger, dir, nil)}
	if err := os.Remove(artifact); err != nil {
		t.Fatalf("remove: %v", err)
	}
	cleanupRec := evidence.Receipt{ToolName: "bash", Success: true, Mutation: true, MutationEvidence: evidence.MutationUnknown}
	decorateObservedPaths(&cleanupRec, cleanup)
	ledger.Record(cleanupRec)

	if !slices.Contains(cleanupRec.Paths, artifact) {
		t.Fatalf("Paths = %v, want the artifact the cleanup removed", cleanupRec.Paths)
	}
	if leftSomethingBehind(ledger, cleanupRec) {
		t.Fatal("removing what this turn built leaves nothing to verify")
	}
}

// Removing a file the turn did not create is the change, not a cleanup.
func TestRemovingAPreexistingFileStillCounts(t *testing.T) {
	dir := testenv.TempDir(t)
	victim := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(victim, []byte("k: v\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	ledger := evidence.NewLedger()
	plan := &toolCallPlan{pathsBefore: snapshotPaths(ledger, dir, nil)}
	if err := os.Remove(victim); err != nil {
		t.Fatalf("remove: %v", err)
	}
	rec := evidence.Receipt{ToolName: "bash", Success: true, Mutation: true, MutationEvidence: evidence.MutationUnknown}
	decorateObservedPaths(&rec, plan)
	ledger.Record(rec)

	if !leftSomethingBehind(ledger, rec) {
		t.Fatal("deleting a file the turn never created is a change to account for")
	}
}

// A probe written under $TMPDIR is not the work product, so it cannot be what
// the turn owes a verification for. A relative path is the workspace by
// construction — every file tool resolves it there — and must still count.
func TestBaselineIgnoresWritesOutsideTheWorkspace(t *testing.T) {
	root := testenv.TempDir(t)
	a := &Agent{}
	a.writeWorkspaceRoot = root

	scratch := evidence.Receipt{
		ToolName: "write_file", Success: true, Write: true, Mutation: true,
		MutationEvidence: evidence.MutationProven,
		Paths:            []string{filepath.Join(testenv.TempDir(t), "probe", "main.go")},
	}
	if a.touchedTheWorkspace(scratch) {
		t.Error("a write outside the workspace was counted as a change to it")
	}

	inside := scratch
	inside.Paths = []string{filepath.Join(root, "internal", "agent", "x.go")}
	if !a.touchedTheWorkspace(inside) {
		t.Error("a write inside the workspace was not counted")
	}

	relative := scratch
	relative.Paths = []string{filepath.Join("internal", "agent", "x.go")}
	if !a.touchedTheWorkspace(relative) {
		t.Error("a relative path resolves against the workspace and must count")
	}

	unnamed := scratch
	unnamed.Paths = nil
	if !a.touchedTheWorkspace(unnamed) {
		t.Error("a change with no named path must be assumed to be the workspace")
	}
}

// A command the host cannot read — sed through its own script language, a path
// built from a variable — is a mutation until the workspace says otherwise.
// Asking after the fact is the one question no reading of the command answers.
func TestUnprovenCallSettlesAgainstTheWorkspace(t *testing.T) {
	root := testenv.TempDir(t)
	if err := os.WriteFile(filepath.Join(root, "tally.go"), []byte("package tally\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := &Agent{}
	a.observeRoot = root

	unproven := func() evidence.Receipt {
		return evidence.Receipt{
			ToolName: "bash", Success: true, Mutation: true,
			MutationEvidence: evidence.MutationUnknown,
			Command:          "sed -n '1,20p' tally.go",
		}
	}
	plan := &toolCallPlan{scanBefore: scanWorkspace(t.Context(), root)}
	if !plan.scanBefore.complete {
		t.Fatal("a small temporary directory should scan completely")
	}

	looked := unproven()
	a.settleUnchangedWorkspace(t.Context(), &looked, plan)
	if looked.Mutation || looked.MutationEvidence != "" {
		t.Errorf("receipt = %+v, want a call that changed nothing settled as no mutation", looked)
	}

	// The same call, against a workspace that did change: the walk answered what
	// the command would not, so the change is proven and names the file.
	if err := os.WriteFile(filepath.Join(root, "tally.go"), []byte("package tally\n\nvar x = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	wrote := unproven()
	a.settleUnchangedWorkspace(t.Context(), &wrote, plan)
	if !wrote.Mutation || wrote.MutationEvidence != evidence.MutationProven {
		t.Errorf("receipt = %+v, want the observed change to establish the mutation's scope", wrote)
	}
	if !holdsPath(wrote.Paths, filepath.Join(root, "tally.go")) {
		t.Errorf("paths = %q, want the file the walk saw change", wrote.Paths)
	}

	// A file created where nothing was watched counts too.
	created := unproven()
	before := &toolCallPlan{scanBefore: scanWorkspace(t.Context(), root)}
	if err := os.WriteFile(filepath.Join(root, "new.go"), []byte("package tally\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a.settleUnchangedWorkspace(t.Context(), &created, before)
	if !created.Mutation {
		t.Error("a file that appeared must leave the mutation standing")
	}
}

// What the watched paths prove is not all a call did: a new top-level entry
// proves a change while a rewrite deeper in the tree goes unseen. Only the walk
// names every file, and only a list it did not cut short is complete.
func TestTheWalkCompletesWhatTheWatchedPathsProved(t *testing.T) {
	root := testenv.TempDir(t)
	lib := filepath.Join(root, "lib", "a.txt")
	if err := os.MkdirAll(filepath.Dir(lib), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lib, []byte("good\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := &Agent{}
	a.observeRoot = root
	plan := &toolCallPlan{scanBefore: scanWorkspace(t.Context(), root), pathsBefore: snapshotPaths(evidence.NewLedger(), root, nil)}
	stamp := filepath.Join(root, ".cache", "stamp")
	if err := os.MkdirAll(filepath.Dir(stamp), 0o755); err != nil {
		t.Fatal(err)
	}
	for path, body := range map[string]string{stamp: "1\n", lib: "clobbered\n"} {
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	rec := evidence.Receipt{ToolName: "bash", Success: true, Mutation: true, MutationEvidence: evidence.MutationUnknown, Command: "sh check.sh"}
	decorateObservedPaths(&rec, plan)
	a.settleUnchangedWorkspace(t.Context(), &rec, plan)
	if !holdsPath(rec.Paths, lib) || holdsPath(rec.Created, lib) {
		t.Fatalf("paths = %q, created = %q, want the rewritten lib/a.txt named and not created", rec.Paths, rec.Created)
	}
	if !holdsPath(rec.Created, stamp) || !rec.PathsComplete {
		t.Fatalf("created = %q, complete = %v, want the new stamp created by a complete walk", rec.Created, rec.PathsComplete)
	}

	crowd := &toolCallPlan{scanBefore: scanWorkspace(t.Context(), root)}
	for i := range observedPathLimit {
		if err := os.WriteFile(filepath.Join(root, ".cache", fmt.Sprintf("f%d", i)), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cut := evidence.Receipt{ToolName: "bash", Success: true, Mutation: true, MutationEvidence: evidence.MutationUnknown, Command: "sh check.sh"}
	a.settleUnchangedWorkspace(t.Context(), &cut, crowd)
	if cut.PathsComplete {
		t.Fatalf("a list cut at %d paths claimed to be complete", observedPathLimit)
	}
}

// An incomplete scan is the one case where the answer is simply unavailable:
// "nothing changed" cannot be read off a tree that was only half walked.
func TestAPartialScanSettlesNothing(t *testing.T) {
	a := &Agent{}
	a.writeWorkspaceRoot = testenv.TempDir(t)
	rec := evidence.Receipt{
		ToolName: "bash", Success: true, Mutation: true,
		MutationEvidence: evidence.MutationUnknown, Command: "make",
	}
	a.settleUnchangedWorkspace(t.Context(), &rec, &toolCallPlan{})
	if !rec.Mutation {
		t.Error("a call with no before-scan must keep its mutation")
	}
	if (workspaceScan{complete: true}).unchanged(workspaceScan{}) {
		t.Error("an incomplete after-scan must never read as unchanged")
	}
	if scanWorkspace(t.Context(), "").complete {
		t.Error("no workspace root is not a complete scan")
	}
}

// The VCS store is not the work product: `git status` alone rewrites the index,
// and a run that only asked about the tree did not change it.
func TestTheVCSStoreIsNotTheWorkspace(t *testing.T) {
	root := testenv.TempDir(t)
	store := filepath.Join(root, ".git")
	if err := os.MkdirAll(store, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(store, "index"), []byte("before"), 0o644); err != nil {
		t.Fatal(err)
	}
	before := scanWorkspace(t.Context(), root)
	if err := os.WriteFile(filepath.Join(store, "index"), []byte("after-a-status"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !before.unchanged(scanWorkspace(t.Context(), root)) {
		t.Error("an index rewrite read as a change to the workspace")
	}
}

// A backgrounded job's receipt is written the moment it starts, so the tree it
// is about to write to still looks untouched. It must never settle.
func TestABackgroundJobNeverSettles(t *testing.T) {
	a := &Agent{}
	a.observeRoot = testenv.TempDir(t)
	plan := &toolCallPlan{
		evidenceName: "bash",
		evidenceArgs: []byte(`{"command":"make build","run_in_background":true}`),
	}
	if a.scanBeforeUnprovenCall(t.Context(), plan).complete {
		t.Error("a background job took a before-scan it could never honestly compare")
	}
	foreground := &toolCallPlan{evidenceName: "bash", evidenceArgs: []byte(`{"command":"make build"}`)}
	if !a.scanBeforeUnprovenCall(t.Context(), foreground).complete {
		t.Error("a foreground unprovable call should scan")
	}
}

// The ledger folds path case on Windows and a directory read does not, so the
// same file reaches the snapshot spelled two ways. Watching it twice makes one
// change count as two everywhere downstream reads Receipt.Paths.
func TestOneFileIsWatchedOnceAcrossSpellings(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("only Windows folds path case, so the two spellings are two files elsewhere")
	}
	dir := testenv.TempDir(t)
	path := filepath.Join(dir, "Parse.go")
	if err := os.WriteFile(path, []byte("before\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	ledger := evidence.NewLedger()
	ledger.Record(evidence.Receipt{ToolName: "read_file", Success: true, Read: true, Paths: []string{path}})

	snap := snapshotPaths(ledger, dir, []string{path})
	if len(snap.state) != 1 {
		t.Fatalf("watching %d entries for one file: %v", len(snap.state), snap.state)
	}
	if err := os.WriteFile(path, []byte("after\n"), 0o644); err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	rec := evidence.Receipt{ToolName: "bash", Success: true, Mutation: true, MutationEvidence: evidence.MutationUnknown}
	decorateObservedPaths(&rec, &toolCallPlan{pathsBefore: snap})
	if len(rec.Paths) != 1 {
		t.Fatalf("Paths = %v, want the one file it changed", rec.Paths)
	}
}

// A background writer running beside a call writes into the same tree, so the
// difference between the two walks is not this call's to claim. Nothing settles
// while one is registered, and the call's scope stays unknown.
func TestAnActiveBackgroundWriterSettlesNothing(t *testing.T) {
	root := testenv.TempDir(t)
	a := &Agent{}
	a.observeRoot = root
	a.svc.mutationObserver = checkpoint.NewMutationObserver(checkpoint.ObserverOptions{})
	if err := a.svc.mutationObserver.RegisterWriter("sibling", "background_subagent", 0); err != nil {
		t.Fatal(err)
	}
	plan := &toolCallPlan{evidenceName: "bash", evidenceArgs: []byte(`{"command":"make build"}`)}
	if a.scanBeforeUnprovenCall(t.Context(), plan).complete {
		t.Fatal("a call took a before-scan while another writer was running")
	}
	plan.scanBefore = scanWorkspace(t.Context(), root)
	if err := os.WriteFile(filepath.Join(root, "sibling.go"), []byte("package x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rec := evidence.Receipt{ToolName: "bash", Success: true, Mutation: true, MutationEvidence: evidence.MutationUnknown, Command: "make build"}
	a.settleUnchangedWorkspace(t.Context(), &rec, plan)
	if rec.MutationEvidence != evidence.MutationUnknown || len(rec.Paths) != 0 {
		t.Fatalf("receipt = %+v, want the sibling's write not attributed to this call", rec)
	}
	a.svc.mutationObserver.UnregisterWriter("sibling")
	if !a.scanBeforeUnprovenCall(t.Context(), plan).complete {
		t.Fatal("with no other writer running the call should scan")
	}
}

// A child is built without the parent's write root; the observe root alone is
// what lets its unclassified calls settle.
func TestObserveRootAloneLetsAChildSettle(t *testing.T) {
	root := testenv.TempDir(t)
	child := New(nil, nil, nil, Options{ObserveRoot: root}, nil)
	if child.writeWorkspaceRoot != "" || child.observeRoot != root {
		t.Fatalf("child roots = write %q observe %q, want only the observe root", child.writeWorkspaceRoot, child.observeRoot)
	}
	plan := &toolCallPlan{evidenceName: "bash", evidenceArgs: []byte(`{"command":"sed -i s/a/b/ f.txt"}`)}
	plan.scanBefore = child.scanBeforeUnprovenCall(t.Context(), plan)
	if err := os.WriteFile(filepath.Join(root, "f.txt"), []byte("b"), 0o644); err != nil {
		t.Fatal(err)
	}
	rec := evidence.Receipt{ToolName: "bash", Success: true, Mutation: true, MutationEvidence: evidence.MutationUnknown, Command: "sed -i s/a/b/ f.txt"}
	child.settleUnchangedWorkspace(t.Context(), &rec, plan)
	if rec.MutationEvidence != evidence.MutationProven || !holdsPath(rec.Paths, filepath.Join(root, "f.txt")) {
		t.Fatalf("receipt = %+v, want the child's own write proven by observation", rec)
	}
	if New(nil, nil, nil, Options{WriteWorkspaceRoot: root}, nil).observeRoot != root {
		t.Fatal("an agent with a write root observes it when no observe root is set")
	}
}

// A run that declared its write paths takes a change inside them as its own
// even while siblings write beside it; a change outside them is theirs.
func TestADeclaredScopeAttributesOnlyWhatFallsInsideIt(t *testing.T) {
	root := testenv.TempDir(t)
	mine, theirs := filepath.Join(root, "billing"), filepath.Join(root, "notify")
	for _, d := range []string{mine, theirs} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	claim, err := writeclaim.NormalizeWritePaths(root, []string{"billing"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := writeclaim.WithSubagentWriteGrant(t.Context(), writeclaim.NewWriteGrant(claim))
	a := &Agent{}
	a.observeRoot = root
	a.svc.mutationObserver = checkpoint.NewMutationObserver(checkpoint.ObserverOptions{})
	if err := a.svc.mutationObserver.RegisterWriter("sibling", "background_subagent", 0); err != nil {
		t.Fatal(err)
	}
	unknown := func() evidence.Receipt {
		return evidence.Receipt{ToolName: "bash", Success: true, Mutation: true, MutationEvidence: evidence.MutationUnknown, Command: "sed -i s/a/b/ total.py"}
	}
	plan := &toolCallPlan{evidenceName: "bash", evidenceArgs: []byte(`{"command":"sed -i s/a/b/ total.py"}`)}
	plan.scanBefore = a.scanBeforeUnprovenCall(ctx, plan)
	if !plan.scanBefore.complete {
		t.Fatal("a run with declared paths should walk even beside another writer")
	}
	writeTo := func(p string) {
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeTo(filepath.Join(mine, "total.py"))
	writeTo(filepath.Join(theirs, "subject.py"))
	rec := unknown()
	a.settleUnchangedWorkspace(ctx, &rec, plan)
	if rec.MutationEvidence != evidence.MutationProven || len(rec.Paths) != 1 || !holdsPath(rec.Paths, filepath.Join(mine, "total.py")) {
		t.Fatalf("receipt = %+v, want only the write inside billing attributed", rec)
	}

	outside := &toolCallPlan{scanBefore: scanWorkspace(t.Context(), root)}
	writeTo(filepath.Join(theirs, "other.py"))
	onlyTheirs := unknown()
	a.settleUnchangedWorkspace(ctx, &onlyTheirs, outside)
	if onlyTheirs.MutationEvidence != evidence.MutationUnknown || !onlyTheirs.Mutation {
		t.Fatalf("receipt = %+v, want a call that only saw a sibling's write to stay unknown, never unchanged", onlyTheirs)
	}
}

// A background child is itself a registered writer; it is not another writer
// beside its own calls.
func TestABackgroundChildIsNotItsOwnSibling(t *testing.T) {
	parent := checkpoint.NewMutationObserver(checkpoint.ObserverOptions{})
	child := parent.CloneForSubagent("subagent:c1", 0, true)
	if err := child.RegisterWriter("subagent:c1", "background_subagent", 0); err != nil {
		t.Fatal(err)
	}
	a := &Agent{}
	a.svc.mutationObserver = child
	if !a.mayAttributeObserved(t.Context()) {
		t.Fatal("a background child alone was treated as running beside another writer")
	}
	if err := parent.RegisterWriter("subagent:c2", "background_subagent", 0); err != nil {
		t.Fatal(err)
	}
	if a.mayAttributeObserved(t.Context()) {
		t.Fatal("with a sibling running and no declared scope, nothing may be attributed")
	}
}
