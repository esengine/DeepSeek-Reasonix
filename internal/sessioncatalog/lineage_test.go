package sessioncatalog

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/provider"
)

func saveLineageSession(t *testing.T, path string, messages ...string) {
	t.Helper()
	s := agent.NewSession("sys")
	for i, message := range messages {
		role := provider.RoleUser
		if i%2 == 1 {
			role = provider.RoleAssistant
		}
		s.Add(provider.Message{Role: role, Content: message})
	}
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
}

func TestClassifyRecoveryLineageUsesParentDirectory(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "root.jsonl")
	saveLineageSession(t, root, "q", "a", "continued", "done")
	branchSession := agent.NewSession("sys")
	branchSession.Add(provider.Message{Role: provider.RoleUser, Content: "q"})
	branchSession.Add(provider.Message{Role: provider.RoleAssistant, Content: "a"})
	info, err := branchSession.SaveConflictRecoveryBranch(agent.RecoveryBranchOptions{OriginalPath: root})
	if err != nil {
		t.Fatal(err)
	}
	covered := classifyRecoveryLineage(SessionRecord{
		Path: info.Path, Recovered: true, ParentID: agent.BranchID(root),
	})
	if covered.RecoveryRole != RecoveryRoleCoveredCopy || !covered.RecoveryCopy {
		t.Fatalf("covered = %+v", covered)
	}
	normal := classifyRecoveryLineage(SessionRecord{Path: root})
	if normal.RecoveryRole != RecoveryRoleNormal {
		t.Fatalf("normal role = %q", normal.RecoveryRole)
	}
}

func TestPromoteCanonicalLeavesRequiresContentCoverage(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "root.jsonl")
	leaf := filepath.Join(dir, "leaf.jsonl")
	saveLineageSession(t, root, "q", "a")
	saveLineageSession(t, leaf, "q", "a", "next", "answer")
	recs := []SessionRecord{
		{Path: root, RecoveryRole: RecoveryRoleNormal, Turns: 1, TurnsState: TurnsValid},
		{Path: leaf, Recovered: true, ParentID: "root", RecoveryGroupID: "root", RecoveryRole: RecoveryRoleDiverged, Turns: 2, TurnsState: TurnsValid},
	}
	out := promoteCanonicalLeaves(recs)
	if !out[1].RecoveryCanonical || out[1].RecoveryRole != RecoveryRoleAdopted {
		t.Fatalf("unique covering leaf = %+v", out[1])
	}

	peer := filepath.Join(dir, "peer.jsonl")
	saveLineageSession(t, peer, "q", "a", "other", "branch")
	recs = append(recs, SessionRecord{
		Path: peer, Recovered: true, ParentID: "root", RecoveryGroupID: "root", RecoveryRole: RecoveryRoleDiverged, Turns: 2, TurnsState: TurnsValid,
	})
	out = promoteCanonicalLeaves(recs)
	canonical := 0
	for _, record := range out[1:] {
		if record.RecoveryRole != RecoveryRoleDiverged {
			t.Fatalf("ambiguous equal-length leaves must keep diverged role: %+v", out)
		}
		if record.RecoveryCanonical {
			canonical++
		}
	}
	// Ordinary list still needs exactly one stable representative even when
	// content truly forks; History remains the place to open the other leaf.
	if canonical != 1 {
		t.Fatalf("ambiguous group canonical count = %d, want 1 stable representative: %+v", canonical, out)
	}
}

func TestPromoteCanonicalLeavesIgnoresUnknownTurnMetadataAndCoversAncestors(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "root.jsonl")
	ancestor := filepath.Join(dir, "ancestor.jsonl")
	leaf := filepath.Join(dir, "leaf.jsonl")
	saveLineageSession(t, root, "q", "a")
	saveLineageSession(t, ancestor, "q", "a", "next", "one")
	saveLineageSession(t, leaf, "q", "a", "next", "one", "again", "two")
	recs := []SessionRecord{
		{Path: root, RecoveryRole: RecoveryRoleNormal, TurnsState: TurnsUnknown},
		{Path: ancestor, Recovered: true, ParentID: "root", RecoveryGroupID: "root", RecoveryRole: RecoveryRoleDiverged, TurnsState: TurnsUnknown},
		{Path: leaf, Recovered: true, ParentID: "ancestor", RecoveryGroupID: "root", RecoveryRole: RecoveryRoleDiverged, TurnsState: TurnsUnknown},
	}
	out := promoteCanonicalLeaves(recs)
	if !out[2].RecoveryCanonical || out[2].RecoveryRole != RecoveryRoleAdopted {
		t.Fatalf("leaf = %+v, want adopted despite unknown turns", out[2])
	}
	if !out[1].RecoveryCopy || out[1].RecoveryRole != RecoveryRoleCoveredCopy {
		t.Fatalf("ancestor = %+v, want covered copy", out[1])
	}
}

func TestCanonicalSessionPathForTopic(t *testing.T) {
	sessions := []SessionRecord{
		{Path: "/s/old.jsonl", RecoveryRole: RecoveryRoleNormal},
		{Path: "/s/leaf.jsonl", RecoveryRole: RecoveryRoleAdopted, RecoveryCanonical: true},
	}
	if got := CanonicalSessionPathForTopic(sessions, "/s/old.jsonl"); got != "/s/leaf.jsonl" {
		t.Fatalf("retarget = %q", got)
	}
	if got := CanonicalSessionPathForTopic(sessions, "/s/leaf.jsonl"); got != "" {
		t.Fatalf("already canonical should not retarget: %q", got)
	}
}

func TestOrdinaryContinuePathFollowsParentOnly(t *testing.T) {
	parent := "/s/old.jsonl"
	leaf := "/s/leaf.jsonl"
	fork := "/s/fork.jsonl"
	sessions := []SessionRecord{
		{Path: parent, RecoveryRole: RecoveryRoleNormal},
		{Path: leaf, Recovered: true, RecoveryRole: RecoveryRoleAdopted, RecoveryCanonical: true},
		{Path: fork, Recovered: true, RecoveryRole: RecoveryRoleDiverged},
	}
	if got := OrdinaryContinuePath(sessions, parent); got != leaf {
		t.Fatalf("parent continue = %q, want leaf", got)
	}
	if got := OrdinaryContinuePath(sessions, leaf); got != "" {
		t.Fatalf("leaf continue = %q, want keep", got)
	}
	if got := OrdinaryContinuePath(sessions, fork); got != "" {
		t.Fatalf("fork continue = %q, want keep inspection path", got)
	}
	if got := OrdinaryContinuePath(sessions, "/s/stale-parent.jsonl"); got != leaf {
		t.Fatalf("stale parent continue = %q, want leaf", got)
	}
}

func TestOrdinaryContinuePathFollowsUniqueLinearCompactedLeaf(t *testing.T) {
	root := filepath.Join("/s", "root.jsonl")
	parentID := "root"
	sessions := []SessionRecord{{
		Path: root, RecoveryRole: RecoveryRoleNormal,
		Turns: 9, TurnsState: TurnsValid, LastActivityAt: 1,
	}}
	for i := 1; i <= 130; i++ {
		id := fmt.Sprintf("recovery-%03d", i)
		sessions = append(sessions, SessionRecord{
			Path: filepath.Join("/s", id+".jsonl"), Recovered: true,
			ParentID: parentID, RecoveryGroupID: "root", RecoveryRole: RecoveryRoleDiverged,
			Turns: 9 + (i*63)/130, TurnsState: TurnsValid, LastActivityAt: int64(i + 1),
		})
		parentID = id
	}
	leaf := sessions[len(sessions)-1].Path
	if got := OrdinaryContinuePath(sessions, root); got != leaf {
		t.Fatalf("parent continue = %q, want unique 72-turn leaf %q", got, leaf)
	}
	if got := topicRepresentativePath(sessions); got != leaf {
		t.Fatalf("representative = %q, want unique 72-turn leaf %q", got, leaf)
	}
	if got := OrdinaryContinuePath(sessions, leaf); got != "" {
		t.Fatalf("leaf continue = %q, want keep current leaf", got)
	}
}

func TestOrdinaryContinuePathRejectsForkedOrRegressingLineage(t *testing.T) {
	root := SessionRecord{Path: "/s/root.jsonl", RecoveryRole: RecoveryRoleNormal, Turns: 9, TurnsState: TurnsValid, LastActivityAt: 1}
	linear := SessionRecord{
		Path: "/s/linear.jsonl", Recovered: true, ParentID: "root", RecoveryGroupID: "root",
		RecoveryRole: RecoveryRoleDiverged, Turns: 10, TurnsState: TurnsValid, LastActivityAt: 2,
	}
	fork := SessionRecord{
		Path: "/s/fork.jsonl", Recovered: true, ParentID: "root", RecoveryGroupID: "root",
		RecoveryRole: RecoveryRoleDiverged, Turns: 11, TurnsState: TurnsValid, LastActivityAt: 3,
	}
	if got := OrdinaryContinuePath([]SessionRecord{root, linear, fork}, root.Path); got != "" {
		t.Fatalf("forked continue = %q, want unresolved", got)
	}
	regressed := linear
	regressed.Turns = 8
	if got := OrdinaryContinuePath([]SessionRecord{root, regressed}, root.Path); got != "" {
		t.Fatalf("regressing continue = %q, want unresolved", got)
	}
}

func TestExplicitPreferredRecoveryWinsWithoutMakingPeersCovered(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "root.jsonl")
	left := filepath.Join(dir, "left.jsonl")
	right := filepath.Join(dir, "right.jsonl")
	saveLineageSession(t, root, "q", "a")
	saveLineageSession(t, left, "q", "a", "left", "answer")
	saveLineageSession(t, right, "q", "a", "right", "answer")
	records := []SessionRecord{
		{Path: root, RecoveryRole: RecoveryRoleNormal},
		{Path: left, Recovered: true, ParentID: "root", RecoveryGroupID: "root", RecoveryRole: RecoveryRoleDiverged, RecoveryPreferred: true},
		{Path: right, Recovered: true, ParentID: "root", RecoveryGroupID: "root", RecoveryRole: RecoveryRoleDiverged},
	}
	out := promoteCanonicalLeaves(records)
	if !out[1].RecoveryCanonical || out[1].RecoveryRole != RecoveryRolePreferred {
		t.Fatalf("preferred = %+v", out[1])
	}
	if out[2].RecoveryCopy || out[2].RecoveryRole != RecoveryRoleDiverged {
		t.Fatalf("diverged peer must retain unique content: %+v", out[2])
	}
	if got := CanonicalSessionPathForTopic(out, root); got != left {
		t.Fatalf("canonical path = %q, want %q", got, left)
	}
}

func TestReconcilePersistsContentProvenCanonicalLeaf(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	root := filepath.Join(dir, "root.jsonl")
	saveLineageSession(t, root, "q", "a")
	continued := agent.NewSession("sys")
	continued.Add(provider.Message{Role: provider.RoleUser, Content: "q"})
	continued.Add(provider.Message{Role: provider.RoleAssistant, Content: "a"})
	continued.Add(provider.Message{Role: provider.RoleUser, Content: "next"})
	continued.Add(provider.Message{Role: provider.RoleAssistant, Content: "answer"})
	info, err := continued.SaveConflictRecoveryBranch(agent.RecoveryBranchOptions{OriginalPath: root})
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := Open(ctx, Options{InMemory: true, DisableRepair: true})
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close(ctx)
	if err := catalog.ReconcileDirectory(ctx, DirectoryTarget{Path: dir, Scope: "global"}); err != nil {
		t.Fatal(err)
	}
	record, ok, err := catalog.GetSession(ctx, info.Path)
	if err != nil || !ok {
		t.Fatalf("GetSession ok=%v err=%v", ok, err)
	}
	if record.RecoveryRole != RecoveryRoleAdopted || !record.RecoveryCanonical {
		t.Fatalf("reconciled recovery = %+v, want adopted canonical", record)
	}
}

func TestReconcileOpensUniqueLinearCompactedLeafWithoutAuthorizingCleanup(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	root := filepath.Join(dir, "root.jsonl")
	leaf := filepath.Join(dir, "leaf.jsonl")
	rootMessages := make([]string, 0, 18)
	for i := range 9 {
		rootMessages = append(rootMessages, fmt.Sprintf("root question %d", i), fmt.Sprintf("root answer %d", i))
	}
	leafMessages := make([]string, 0, 144)
	for i := range 72 {
		leafMessages = append(leafMessages, fmt.Sprintf("compacted question %d", i), fmt.Sprintf("compacted answer %d", i))
	}
	saveLineageSession(t, root, rootMessages...)
	saveLineageSession(t, leaf, leafMessages...)
	for path, meta := range map[string]agent.BranchMeta{
		root: {ID: "root", Scope: "global", TopicID: "conversation", TopicTitle: "Compacted"},
		leaf: {ID: "leaf", Scope: "global", TopicID: "conversation", TopicTitle: "Compacted", Recovered: true, ParentID: "root", RecoveryDepth: 1},
	} {
		if err := agent.SaveBranchMetaPreserveUpdated(path, meta); err != nil {
			t.Fatal(err)
		}
	}
	catalog, err := Open(ctx, Options{InMemory: true, DisableRepair: true})
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close(ctx)
	if err := catalog.ReconcileDirectory(ctx, DirectoryTarget{Path: dir, Scope: "global"}); err != nil {
		t.Fatal(err)
	}
	topic, ok, err := catalog.GetTopic(ctx, TopicKey{Scope: "global", TopicID: "conversation"})
	if err != nil || !ok {
		t.Fatalf("GetTopic ok=%v err=%v", ok, err)
	}
	if topic.RepresentativePath != leaf {
		t.Fatalf("representative = %q, want compacted leaf %q", topic.RepresentativePath, leaf)
	}
	if topic.RecoveryCleanupEligibleCount != 0 {
		t.Fatalf("cleanup eligible = %d, want 0 without content coverage", topic.RecoveryCleanupEligibleCount)
	}
	if got := CanonicalSessionPathForTopic(topic.Sessions, root); got != "" {
		t.Fatalf("cleanup canonical = %q, want unresolved", got)
	}
	if got := OrdinaryContinuePath(topic.Sessions, root); got != leaf {
		t.Fatalf("ordinary continue = %q, want compacted leaf %q", got, leaf)
	}
}

func TestReconcileReanchorsCrossTopicRecoveryIntoOneLogicalTopic(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	root := filepath.Join(dir, "root.jsonl")
	copyPath := filepath.Join(dir, "copy.jsonl")
	leaf := filepath.Join(dir, "leaf.jsonl")
	saveLineageSession(t, root, "q", "a")
	saveLineageSession(t, copyPath, "q", "a")
	saveLineageSession(t, leaf, "q", "a", "next", "done")
	for path, meta := range map[string]agent.BranchMeta{
		root:     {ID: "root", Scope: "global", TopicID: "conversation", TopicTitle: "Upgraded"},
		copyPath: {ID: "copy", Scope: "global", TopicID: "legacy-copy-topic", Recovered: true, ParentID: "root", RecoveryDepth: 1},
		leaf:     {ID: "leaf", Scope: "global", TopicID: "legacy-leaf-topic", Recovered: true, ParentID: "copy", RecoveryDepth: 2},
	} {
		if err := agent.SaveBranchMetaPreserveUpdated(path, meta); err != nil {
			t.Fatal(err)
		}
	}
	catalog, err := Open(ctx, Options{InMemory: true, DisableRepair: true})
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close(ctx)
	if err := catalog.ReconcileDirectory(ctx, DirectoryTarget{Path: dir, Scope: "global"}); err != nil {
		t.Fatal(err)
	}
	page, err := catalog.ListTopics(ctx, TopicPageRequest{Scope: "global", Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].TopicID != "conversation" {
		t.Fatalf("ListTopics = %+v, want one logical topic conversation", page.Items)
	}
	leafRec, ok, err := catalog.GetSession(ctx, leaf)
	if err != nil || !ok {
		t.Fatalf("GetSession leaf ok=%v err=%v", ok, err)
	}
	if leafRec.TopicID != "conversation" || leafRec.LogicalTopicID != "conversation" {
		t.Fatalf("leaf projection = %+v, want re-anchored to conversation", leafRec)
	}
	if leafRec.OrdinaryVisible {
		t.Fatalf("leaf must not be ordinary-visible while root exists: %+v", leafRec)
	}
	rootRec, ok, err := catalog.GetSession(ctx, root)
	if err != nil || !ok || !rootRec.OrdinaryVisible {
		t.Fatalf("root ordinary visibility = %+v ok=%v err=%v", rootRec, ok, err)
	}
}

func TestRecoveryFilenameParentID(t *testing.T) {
	parent, ok := agent.RecoveryFilenameParentID("/s/chat-recovery-0123456789abcdef.jsonl")
	if !ok || parent != "chat" {
		t.Fatalf("parent = %q ok=%v", parent, ok)
	}
	if !agent.LooksLikeRecoveryFilename("/s/chat-recovery-0123456789abcdef.jsonl") {
		t.Fatal("expected recovery filename")
	}
	if agent.LooksLikeRecoveryFilename("/s/chat.jsonl") {
		t.Fatal("normal session must not look like recovery")
	}
}

func TestUpgradeMatrixV4RebuildKeepsSingleLogicalRowAndAuthority(t *testing.T) {
	// Simulates 1.24.2-style multi-topic recovery storm → new v6 projection →
	// discard cache → reindex. Ordinary list stays one row; JSONL/meta bytes
	// are never rewritten (the 1.23.0→new-version reinstall path).
	ctx := context.Background()
	dir := t.TempDir()
	cacheDir := t.TempDir()
	root := filepath.Join(dir, "root.jsonl")
	copyPath := filepath.Join(dir, "copy.jsonl")
	leaf := filepath.Join(dir, "leaf.jsonl")
	saveLineageSession(t, root, "q", "a")
	saveLineageSession(t, copyPath, "q", "a")
	saveLineageSession(t, leaf, "q", "a", "next", "done")
	for path, meta := range map[string]agent.BranchMeta{
		root:     {ID: "root", Scope: "global", TopicID: "conversation", TopicTitle: "Upgraded"},
		copyPath: {ID: "copy", Scope: "global", TopicID: "legacy-copy-topic", Recovered: true, ParentID: "root", RecoveryDepth: 1},
		leaf:     {ID: "leaf", Scope: "global", TopicID: "legacy-leaf-topic", Recovered: true, ParentID: "copy", RecoveryDepth: 2},
	} {
		if err := agent.SaveBranchMetaPreserveUpdated(path, meta); err != nil {
			t.Fatal(err)
		}
	}
	hash := func(path string) string {
		t.Helper()
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	before := map[string]string{}
	for _, path := range []string{root, copyPath, leaf, agent.BranchMetaPath(root), agent.BranchMetaPath(copyPath), agent.BranchMetaPath(leaf)} {
		before[path] = hash(path)
	}

	openAndList := func(label string) (topicID, rep string) {
		t.Helper()
		catalog, err := Open(ctx, Options{Path: filepath.Join(cacheDir, label+".sqlite"), DisableRepair: true})
		if err != nil {
			t.Fatal(err)
		}
		defer catalog.Close(ctx)
		if err := catalog.ReconcileDirectory(ctx, DirectoryTarget{Path: dir, Scope: "global"}); err != nil {
			t.Fatal(err)
		}
		page, err := catalog.ListTopics(ctx, TopicPageRequest{Scope: "global", Limit: 50})
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Items) != 1 {
			t.Fatalf("%s ListTopics = %+v, want one logical row", label, page.Items)
		}
		return page.Items[0].TopicID, page.Items[0].RepresentativePath
	}

	topic1, rep1 := openAndList("pass1")
	topic2, rep2 := openAndList("pass2")
	if topic1 != "conversation" || topic2 != "conversation" {
		t.Fatalf("logical topics = %q/%q, want conversation both rebuilds", topic1, topic2)
	}
	if rep1 == "" || rep1 != rep2 {
		t.Fatalf("representative unstable across rebuilds: %q vs %q", rep1, rep2)
	}
	for path, want := range before {
		if got := hash(path); got != want {
			t.Fatalf("authority file mutated during catalog rebuild: %s", path)
		}
	}
	// v6 path is independent of all older disposable caches.
	if !strings.HasSuffix(filepath.ToSlash(DefaultPath()), "session-catalog/v6.sqlite") && DefaultPath() != "" {
		t.Fatalf("DefaultPath = %q, want v6.sqlite", DefaultPath())
	}
}

func TestReconcileFoldsFilenameRecoveryIntoRootTopic(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	root := filepath.Join(dir, "normal.jsonl")
	recovery := filepath.Join(dir, "normal-recovery-0123456789abcdef.jsonl")
	saveLineageSession(t, root, "normal imported prompt")
	saveLineageSession(t, recovery, "legacy recovery prompt")
	if err := agent.SaveBranchMetaPreserveUpdated(root, agent.BranchMeta{
		ID: "normal", Scope: "global", TopicID: "legacy_normal", TopicTitle: "normal",
	}); err != nil {
		t.Fatal(err)
	}
	if err := agent.SaveBranchMetaPreserveUpdated(recovery, agent.BranchMeta{
		ID: "normal-recovery-0123456789abcdef", Scope: "global",
		TopicID: "legacy_recovery", TopicTitle: "recovery",
	}); err != nil {
		t.Fatal(err)
	}
	catalog, err := Open(ctx, Options{InMemory: true, DisableRepair: true})
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close(ctx)
	if err := catalog.ReconcileDirectory(ctx, DirectoryTarget{Path: dir, Scope: "global"}); err != nil {
		t.Fatal(err)
	}
	page, err := catalog.ListTopics(ctx, TopicPageRequest{Scope: "global", Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].TopicID != "legacy_normal" {
		t.Fatalf("ListTopics = %+v, want one folded legacy_normal topic", page.Items)
	}
	rec, ok, err := catalog.GetSession(ctx, recovery)
	if err != nil || !ok {
		t.Fatalf("GetSession recovery ok=%v err=%v", ok, err)
	}
	if !rec.Recovered || rec.TopicID != "legacy_normal" {
		t.Fatalf("recovery projection = %+v, want re-anchored recovered row", rec)
	}
}
