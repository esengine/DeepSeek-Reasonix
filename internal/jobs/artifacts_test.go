package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/evidence"
)

func TestCompletedJobPersistsOutputAndReleasesMemory(t *testing.T) {
	sessionPath := filepath.Join(t.TempDir(), "session.jsonl")
	m := NewManager(event.Discard)
	defer m.Close()
	m.SetActiveSessionPath("session", sessionPath)

	j := m.StartForSession("session", "bash", "persist", func(_ context.Context, out io.Writer) (string, error) {
		_, _ = io.WriteString(out, strings.Repeat("x", defaultTailBytes+1024))
		return "", nil
	})
	<-j.done

	j.mu.Lock()
	tailLen := len(j.tail)
	result := j.result
	artifactPath := j.artifactPath
	j.mu.Unlock()

	if tailLen != 0 {
		t.Fatalf("completed artifact-backed job kept %d tail bytes, want 0", tailLen)
	}
	if result != "" {
		t.Fatalf("completed artifact-backed job kept result %q, want empty", result)
	}
	if artifactPath == "" {
		t.Fatal("artifact path should be set")
	}

	res := m.WaitForSession(context.Background(), "session", []string{j.ID}, 1)
	if len(res) != 1 || len(res[0].Output) != defaultTailBytes+1024 {
		t.Fatalf("wait output len = %d, want %d", len(res[0].Output), defaultTailBytes+1024)
	}
}

func TestJobArtifactPreservesOutput(t *testing.T) {
	sessionPath := filepath.Join(t.TempDir(), "session.jsonl")
	m := NewManager(event.Discard)
	defer m.Close()
	m.SetActiveSessionPath("session", sessionPath)
	secret := "sk-real-secret-value-123456"

	j := m.StartForSession("session", "bash", "persist secret", func(_ context.Context, out io.Writer) (string, error) {
		_, _ = io.WriteString(out, "DEEPSEEK_API_KEY="+secret+"\n")
		return "Authorization: Bearer ghp_abcdefghijklmnopqrstuvwxyz", nil
	})
	<-j.done

	res := m.WaitForSession(context.Background(), "session", []string{j.ID}, 1)
	if len(res) != 1 {
		t.Fatalf("wait result = %+v", res)
	}
	if !strings.Contains(res[0].Output, secret) || !strings.Contains(res[0].Output, "ghp_abcdefghijklmnopqrstuvwxyz") {
		t.Fatalf("wait output did not preserve job output:\n%s", res[0].Output)
	}

	data, err := os.ReadFile(filepath.Join(ArtifactDir(sessionPath), j.ID+jobLogExt))
	if err != nil {
		t.Fatalf("read artifact: %v", err)
	}
	if !strings.Contains(string(data), secret) || !strings.Contains(string(data), "ghp_abcdefghijklmnopqrstuvwxyz") {
		t.Fatalf("artifact did not preserve job output:\n%s", data)
	}
}

func TestJobArtifactMetadataPreservesLabel(t *testing.T) {
	sessionPath := filepath.Join(t.TempDir(), "session.jsonl")
	m := NewManager(event.Discard)
	defer m.Close()
	m.SetActiveSessionPath("session", sessionPath)
	const secret = "sk-real-secret-value-123456"

	j := m.StartForSession("session", "bash", "echo DEEPSEEK_API_KEY="+secret, func(context.Context, io.Writer) (string, error) {
		return "", nil
	})
	<-j.done

	data, err := os.ReadFile(filepath.Join(ArtifactDir(sessionPath), j.ID+jobMetaExt))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), secret) {
		t.Fatalf("job metadata did not preserve label:\n%s", data)
	}
}

func TestJobArtifactUsesPrivatePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows file ACLs are not represented by Unix permission bits")
	}
	sessionPath := filepath.Join(t.TempDir(), "session.jsonl")
	m := NewManager(event.Discard)
	defer m.Close()
	m.SetActiveSessionPath("session", sessionPath)

	// Simulate a legacy artifact at the path the first job will reuse.
	dir := ArtifactDir(sessionPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(dir, "bash-1"+jobLogExt)
	if err := os.WriteFile(logPath, []byte("old redacted output"), 0o644); err != nil {
		t.Fatal(err)
	}

	j := m.StartForSession("session", "bash", "echo API_KEY=raw-secret", func(_ context.Context, out io.Writer) (string, error) {
		_, _ = io.WriteString(out, "API_KEY=raw-secret\n")
		return "", nil
	})
	<-j.done
	if j.ID != "bash-1" {
		t.Fatalf("job id = %q, want bash-1", j.ID)
	}
	assertPrivateArtifactMode(t, logPath)
	assertPrivateArtifactMode(t, filepath.Join(dir, j.ID+jobMetaExt))
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o700 {
		t.Fatalf("artifact dir mode = %04o, want 0700", got)
	}
}

func assertPrivateArtifactMode(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("%s mode = %04o, want 0600", path, got)
	}
}

func TestRestoreSessionArtifactsAndAdvanceSequence(t *testing.T) {
	sessionPath := filepath.Join(t.TempDir(), "session.jsonl")
	first := NewManager(event.Discard)
	first.SetActiveSessionPath("session", sessionPath)
	j := first.StartForSession("session", "task", "answer", func(context.Context, io.Writer) (string, error) {
		return "persisted answer", nil
	})
	<-j.done
	first.Close()

	second := NewManager(event.Discard)
	defer second.Close()
	second.SetActiveSessionPath("session", sessionPath)

	res := second.WaitForSession(context.Background(), "session", []string{j.ID}, 1)
	if len(res) != 1 || !strings.Contains(res[0].Output, "persisted answer") {
		t.Fatalf("restored wait = %+v, want persisted answer", res)
	}
	if got := second.WaitForSession(context.Background(), "session", nil, 1); len(got) != 0 {
		t.Fatalf("wait without ids should ignore restored completed artifacts, got %+v", got)
	}
	if got := second.LeaseEvidenceForSession("session", j.ID); len(got.Receipts) != 0 {
		t.Fatalf("mutation-free task restored mutation evidence: %+v", got)
	}

	next := second.StartForSession("session", "bash", "next", func(context.Context, io.Writer) (string, error) {
		return "", nil
	})
	<-next.done
	if next.ID == j.ID {
		t.Fatalf("new job reused restored id %q", next.ID)
	}
}

func TestTaskMutationEvidencePersistsWithoutSensitiveReceiptData(t *testing.T) {
	sessionPath := filepath.Join(t.TempDir(), "session.jsonl")
	first := NewManager(event.Discard)
	first.SetActiveSessionPath("session", sessionPath)
	const secret = "private-receipt-value-123456"
	j := first.StartForSession("session", "task", "writer", func(ctx context.Context, _ io.Writer) (string, error) {
		PublishEvidence(ctx, evidence.ChildEvidenceSummary{Receipts: []evidence.Receipt{
			{
				ToolName: "write_file",
				Args:     json.RawMessage(`{"path":"internal/agent/task.go","content":"` + secret + `"}`),
				Success:  true,
				Write:    true,
				Mutation: true,
				Paths:    []string{"internal/agent/task.go"},
			},
			{
				ToolName: "bash",
				Success:  true,
				Command:  "go test ./... --token=" + secret,
			},
		}})
		return "persisted answer", nil
	})
	<-j.done
	first.Close()

	metaPath := filepath.Join(ArtifactDir(sessionPath), j.ID+jobMetaExt)
	data, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, leaked := range []string{secret, "content", "go test ./..."} {
		if strings.Contains(text, leaked) {
			t.Fatalf("job metadata persisted sensitive receipt data %q:\n%s", leaked, text)
		}
	}
	for _, want := range []string{`"mutationEvidenceVersion": 1`, `"risk": "medium"`, `"internal/agent/task.go"`} {
		if !strings.Contains(text, want) {
			t.Fatalf("job metadata missing %q:\n%s", want, text)
		}
	}

	second := NewManager(event.Discard)
	defer second.Close()
	second.SetActiveSessionPath("session", sessionPath)
	summary := second.LeaseEvidenceForSession("session", j.ID)
	if len(summary.Receipts) != 1 {
		t.Fatalf("restored evidence = %+v, want one synthetic mutation", summary)
	}
	receipt := summary.Receipts[0]
	if !receipt.Success || !receipt.Mutation || !receipt.Write || receipt.ToolName != recoveredBackgroundTaskToolName {
		t.Fatalf("restored receipt = %+v, want successful recovered mutation", receipt)
	}
	if len(receipt.Paths) != 1 || filepath.ToSlash(receipt.Paths[0]) != "internal/agent/task.go" {
		t.Fatalf("restored paths = %v, want internal/agent/task.go", receipt.Paths)
	}
	if len(receipt.Args) != 0 || receipt.Command != "" || receipt.Read {
		t.Fatalf("restored receipt retained stale sign-off data: %+v", receipt)
	}

	ledger := evidence.NewLedger()
	ledger.MergeChild(summary)
	mutation, ok := ledger.LatestSuccessfulMutationIndex()
	if !ok || ledger.HasSuccessfulReviewAfter(mutation) || ledger.HasSuccessfulVerificationCommand() {
		t.Fatalf("restored evidence bypassed fresh review/verification: %+v", ledger.Summary())
	}
	if got := ledger.MutationRiskAfter(mutation); got != evidence.RiskMedium {
		t.Fatalf("restored mutation risk = %s, want medium", got)
	}
	// Lease does not consume: the receipts stay available until the collecting
	// turn commits. Only then is the persisted summary drained.
	if again := second.LeaseEvidenceForSession("session", j.ID); len(again.Receipts) != 1 {
		t.Fatalf("restored evidence not re-leasable before commit: %+v", again)
	}
	second.CommitEvidenceForSession("session", j.ID)
	if afterCommit := second.LeaseEvidenceForSession("session", j.ID); len(afterCommit.Receipts) != 0 {
		t.Fatalf("committed evidence still leasable: %+v", afterCommit)
	}

	// The commit drained the persisted copy too — a further restart must not
	// offer the same mutation again.
	third := NewManager(event.Discard)
	defer third.Close()
	third.SetActiveSessionPath("session", sessionPath)
	if thirdLease := third.LeaseEvidenceForSession("session", j.ID); len(thirdLease.Receipts) != 0 {
		t.Fatalf("committed evidence resurrected after restart: %+v", thirdLease)
	}
}

func TestHighRiskTaskMutationEvidenceRestoresAsOpaque(t *testing.T) {
	meta := artifactMeta{
		Kind:                    "task",
		MutationEvidenceVersion: mutationEvidenceVersion,
		MutationEvidence: &artifactMutationEvidence{
			Risk:  string(evidence.RiskHigh),
			Paths: []string{"ordinary-looking.go"},
		},
	}
	summary := mutationEvidenceFromArtifact(meta)
	if len(summary.Receipts) != 1 || len(summary.Receipts[0].Paths) != 0 {
		t.Fatalf("high-risk restored evidence = %+v, want opaque mutation", summary)
	}
	ledger := evidence.NewLedger()
	ledger.MergeChild(summary)
	mutation, ok := ledger.LatestSuccessfulMutationIndex()
	if !ok || ledger.MutationRiskAfter(mutation) != evidence.RiskHigh {
		t.Fatalf("high-risk mutation was downgraded during recovery: %+v", ledger.Summary())
	}
}

func TestLegacyTaskArtifactRecoversAsOpaqueHighRiskMutation(t *testing.T) {
	// A pre-feature artifact (no mutationEvidenceVersion) proves only that the
	// mutation state was never recorded — not that the task made no changes. A
	// legacy background writer task collected after upgrade could carry real,
	// unreviewed edits, so recovery must be conservative: an opaque RiskHigh
	// mutation that forces fresh inspection and review rather than silently
	// skipping it.
	sessionPath := filepath.Join(t.TempDir(), "session.jsonl")
	dir := ArtifactDir(sessionPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "task-1"+jobLogExt), []byte("legacy answer"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeMeta(filepath.Join(dir, "task-1"+jobMetaExt), artifactMeta{
		ID:               "task-1",
		Kind:             "task",
		Status:           Done,
		ArtifactComplete: true,
		LogPath:          "task-1" + jobLogExt,
	}); err != nil {
		t.Fatal(err)
	}

	m := NewManager(event.Discard)
	defer m.Close()
	m.SetActiveSessionPath("session", sessionPath)
	summary := m.LeaseEvidenceForSession("session", "task-1")
	if len(summary.Receipts) != 1 || !summary.HasMutation() || len(summary.MutationPaths()) != 0 {
		t.Fatalf("legacy task evidence = %+v, want one opaque mutation", summary)
	}
	ledger := evidence.NewLedger()
	ledger.MergeChild(summary)
	mutation, ok := ledger.LatestSuccessfulMutationIndex()
	if !ok || ledger.MutationRiskAfter(mutation) != evidence.RiskHigh {
		t.Fatalf("legacy task mutation was not recovered conservatively: %+v", ledger.Summary())
	}
}

func TestFutureVersionTaskArtifactRecoversAsOpaqueHighRiskMutation(t *testing.T) {
	// A meta written by a newer build (unknown non-zero version) may contain
	// real evidence in a shape this build cannot parse: recover it as an opaque
	// mutation so downgrade coexistence cannot skip review.
	meta := artifactMeta{
		Kind:                    "task",
		MutationEvidenceVersion: mutationEvidenceVersion + 1,
	}
	summary := mutationEvidenceFromArtifact(meta)
	if len(summary.Receipts) != 1 || !summary.HasMutation() || len(summary.MutationPaths()) != 0 {
		t.Fatalf("future-version evidence = %+v, want one opaque mutation", summary)
	}
	ledger := evidence.NewLedger()
	ledger.MergeChild(summary)
	mutation, ok := ledger.LatestSuccessfulMutationIndex()
	if !ok || ledger.MutationRiskAfter(mutation) != evidence.RiskHigh {
		t.Fatalf("future-version mutation was not recovered conservatively: %+v", ledger.Summary())
	}
}

func TestLeasedEvidenceResurrectsUntilCommitted(t *testing.T) {
	// Collection is provisional. A lease that is never committed — the
	// collecting turn was cancelled, errored, or the process exited before
	// delivery — must leave the mutation recoverable after a restart, so a
	// background change can never ship unreviewed. Only a commit drains the
	// persisted copy.
	sessionPath := filepath.Join(t.TempDir(), "session.jsonl")
	first := NewManager(event.Discard)
	first.SetActiveSessionPath("session", sessionPath)
	j := first.StartForSession("session", "task", "writer", func(ctx context.Context, _ io.Writer) (string, error) {
		PublishEvidence(ctx, evidence.ChildEvidenceSummary{Receipts: []evidence.Receipt{{
			ToolName: "write_file", Success: true, Write: true, Mutation: true, Paths: []string{"changed.go"},
		}}})
		return "done", nil
	})
	<-j.done
	// Lease without committing (the turn never delivered), then restart.
	if leased := first.LeaseEvidenceForSession("session", j.ID); !leased.HasMutation() {
		t.Fatalf("live lease = %+v, want the published mutation", leased)
	}
	first.Close()

	data, err := os.ReadFile(filepath.Join(ArtifactDir(sessionPath), j.ID+jobMetaExt))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"mutationEvidence"`) {
		t.Fatalf("uncommitted lease drained the persisted mutation summary:\n%s", data)
	}

	second := NewManager(event.Discard)
	defer second.Close()
	second.SetActiveSessionPath("session", sessionPath)
	if summary := second.LeaseEvidenceForSession("session", j.ID); !summary.HasMutation() {
		t.Fatalf("uncommitted evidence lost after restart: %+v", summary)
	}
	// Committing after the restart drains it; a further restart offers nothing.
	second.CommitEvidenceForSession("session", j.ID)
	second.Close()
	third := NewManager(event.Discard)
	defer third.Close()
	third.SetActiveSessionPath("session", sessionPath)
	if summary := third.LeaseEvidenceForSession("session", j.ID); len(summary.Receipts) != 0 {
		t.Fatalf("committed evidence resurrected after restart: %+v", summary)
	}
}

func TestFinishDestroySessionPurgesOwnedJobs(t *testing.T) {
	m := NewManager(event.Discard)
	defer m.Close()

	j := m.StartForSession("session", "task", "done", func(context.Context, io.Writer) (string, error) {
		return "answer", nil
	})
	<-j.done

	done := m.DestroySession("session")
	if len(done) != 0 {
		t.Fatalf("finished job should not need destroy wait, got %d handles", len(done))
	}
	m.FinishDestroySession("session")

	if _, _, ok := m.OutputForSession("session", j.ID); ok {
		t.Fatalf("destroyed session job %s should be purged", j.ID)
	}
}

func TestSetActiveSessionPathMigratesRunningJobArtifacts(t *testing.T) {
	sessionPath := filepath.Join(t.TempDir(), "session.jsonl")
	m := NewManager(event.Discard)
	defer m.Close()

	wroteBefore := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	j := m.StartForSession("session", "bash", "migrate", func(_ context.Context, out io.Writer) (string, error) {
		_, _ = io.WriteString(out, "before\n")
		close(wroteBefore)
		<-release
		_, _ = io.WriteString(out, "after\n")
		return "", nil
	})
	<-wroteBefore
	j.mu.Lock()
	oldPath := j.artifactPath
	j.mu.Unlock()

	m.SetActiveSessionPath("session", sessionPath)
	j.mu.Lock()
	gotPath := j.artifactPath
	j.mu.Unlock()
	if gotPath != oldPath {
		t.Fatalf("running artifact path = %q, want unchanged %q before completion", gotPath, oldPath)
	}

	releaseOnce.Do(func() { close(release) })
	<-j.done
	j.mu.Lock()
	donePath := j.artifactPath
	j.mu.Unlock()
	if !strings.HasPrefix(donePath, ArtifactDir(sessionPath)+string(filepath.Separator)) {
		t.Fatalf("completed artifact path = %q, want under %q", donePath, ArtifactDir(sessionPath))
	}
	res := m.WaitForSession(context.Background(), "session", []string{j.ID}, 1)
	if len(res) != 1 || !strings.Contains(res[0].Output, "before\n") || !strings.Contains(res[0].Output, "after\n") {
		t.Fatalf("wait after migration = %+v, want before and after output", res)
	}
}

func TestArtifactFailureDoesNotFailSuccessfulJob(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "session.jsonl")
	if err := os.WriteFile(ArtifactDir(sessionPath), []byte("not a dir"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := NewManager(event.Discard)
	defer m.Close()
	m.SetActiveSessionPath("session", sessionPath)

	j := m.StartForSession("session", "task", "artifact fail", func(context.Context, io.Writer) (string, error) {
		return "successful result", nil
	})
	<-j.done

	res := m.WaitForSession(context.Background(), "session", []string{j.ID}, 1)
	if len(res) != 1 || res[0].Status != Done {
		t.Fatalf("wait = %+v, want one done result", res)
	}
	if !strings.Contains(res[0].Output, "successful result") || !strings.Contains(res[0].Output, "job artifact incomplete:") {
		t.Fatalf("output = %q, want result and artifact warning", res[0].Output)
	}
}

func TestMigrateArtifactDirFallsBackToCopyWhenRenameFails(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "bash-1.log"), []byte("persisted output"), 0o644); err != nil {
		t.Fatal(err)
	}

	oldRename := renamePath
	renamePath = func(_, _ string) error {
		return errors.New("forced rename failure")
	}
	t.Cleanup(func() { renamePath = oldRename })

	if err := migrateArtifactDir(src, dst); err != nil {
		t.Fatalf("migrateArtifactDir: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dst, "bash-1.log"))
	if err != nil {
		t.Fatalf("read migrated artifact: %v", err)
	}
	if string(got) != "persisted output" {
		t.Fatalf("migrated artifact = %q, want persisted output", got)
	}
	if runtime.GOOS != "windows" {
		assertPrivateArtifactMode(t, filepath.Join(dst, "bash-1.log"))
	}
	if _, err := os.Stat(filepath.Join(src, "bash-1.log")); !os.IsNotExist(err) {
		t.Fatalf("source artifact should be removed after copy fallback, stat err = %v", err)
	}
}

func TestSetActiveSessionPathAdoptsUnscopedTemporaryJobs(t *testing.T) {
	sessionPath := filepath.Join(t.TempDir(), "session.jsonl")
	m := NewManager(event.Discard)
	defer m.Close()

	j := m.StartForSession("", "task", "temporary", func(context.Context, io.Writer) (string, error) {
		return "temporary answer", nil
	})
	<-j.done

	m.SetActiveSessionPath("session", sessionPath)
	res := m.WaitForSession(context.Background(), "session", []string{j.ID}, 1)
	if len(res) != 1 || !strings.Contains(res[0].Output, "temporary answer") {
		t.Fatalf("adopted wait = %+v, want temporary answer", res)
	}
	if _, _, ok := m.OutputForSession("", j.ID); !ok {
		t.Fatalf("legacy unscoped lookup should still find adopted job %s", j.ID)
	}
	if _, err := os.Stat(filepath.Join(ArtifactDir(sessionPath), j.ID+jobLogExt)); err != nil {
		t.Fatalf("adopted artifact should be under persistent sidecar: %v", err)
	}
}

func TestSetActiveSessionPathAdoptsUnscopedJobsOnMigrationFailure(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "session.jsonl")
	if err := os.WriteFile(ArtifactDir(sessionPath), []byte("not a dir"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := NewManager(event.Discard)
	defer m.Close()

	j := m.StartForSession("", "task", "temporary", func(context.Context, io.Writer) (string, error) {
		return "temporary answer", nil
	})
	<-j.done

	m.SetActiveSessionPath("session", sessionPath)

	out, status, ok := m.OutputForSession("session", j.ID)
	if !ok || status != Done {
		t.Fatalf("scoped output ok/status = %v/%s, want true/done", ok, status)
	}
	if !strings.Contains(out, "temporary answer") || !strings.Contains(out, "job artifact incomplete: migration:") {
		t.Fatalf("scoped output = %q, want answer and migration error", out)
	}
	res := m.WaitForSession(context.Background(), "session", []string{j.ID}, 1)
	if len(res) != 1 || !strings.Contains(res[0].Output, "job artifact incomplete: migration:") {
		t.Fatalf("scoped wait = %+v, want migration error", res)
	}
	if note := m.DrainCompletedNoteForSession("session"); !strings.Contains(note, j.ID) {
		t.Fatalf("adopted completion note = %q, want job id %s", note, j.ID)
	}
}

func TestSetActiveSessionPathReportsMigrationFailure(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "session.jsonl")
	if err := os.WriteFile(ArtifactDir(sessionPath), []byte("not a dir"), 0o644); err != nil {
		t.Fatal(err)
	}
	var notices []event.Event
	m := NewManager(event.FuncSink(func(e event.Event) {
		if e.Kind == event.Notice {
			notices = append(notices, e)
		}
	}))
	defer m.Close()

	j := m.StartForSession("session", "task", "migrate fail", func(context.Context, io.Writer) (string, error) {
		return "answer", nil
	})
	m.SetActiveSessionPath("session", sessionPath)
	<-j.done

	res := m.WaitForSession(context.Background(), "session", []string{j.ID}, 1)
	if len(res) != 1 || !strings.Contains(res[0].Output, "job artifact incomplete: migration:") {
		t.Fatalf("wait after migration failure = %+v, want artifact error", res)
	}
	found := false
	for _, notice := range notices {
		if notice.Text == "Job artifact migration failed." && strings.Contains(notice.Detail, "job artifact migration failed") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("migration failure notice not emitted, got %+v", notices)
	}
}

func TestOutputReadsArtifactFromOffset(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bash-1.log")
	prefix := strings.Repeat("x", 2*defaultTailBytes)
	suffix := "new output\n"
	if err := os.WriteFile(path, []byte(prefix+suffix), 0o644); err != nil {
		t.Fatal(err)
	}

	m := NewManager(event.Discard)
	defer m.Close()
	j := &Job{
		ID:           "bash-1",
		Kind:         "bash",
		SessionID:    "session",
		status:       Running,
		readOffset:   int64(len(prefix)),
		artifactPath: path,
		done:         make(chan struct{}),
	}
	m.jobs[jobKey("session", j.ID)] = j
	m.order = append(m.order, jobKey("session", j.ID))

	text, status, ok := m.OutputForSession("session", j.ID)
	if !ok || status != Running {
		t.Fatalf("OutputForSession ok/status = %v/%s, want true/running", ok, status)
	}
	if text != suffix {
		t.Fatalf("OutputForSession text = %q, want %q", text, suffix)
	}
	if j.readOffset != int64(len(prefix)+len(suffix)) {
		t.Fatalf("readOffset = %d, want %d", j.readOffset, len(prefix)+len(suffix))
	}
}

func TestRestoredArtifactsAreScopedBySession(t *testing.T) {
	root := t.TempDir()
	pathA := filepath.Join(root, "a.jsonl")
	pathB := filepath.Join(root, "b.jsonl")

	first := NewManager(event.Discard)
	first.SetActiveSessionPath("session-a", pathA)
	jobA := first.StartForSession("session-a", "bash", "a", func(_ context.Context, out io.Writer) (string, error) {
		_, _ = io.WriteString(out, "from-a")
		return "", nil
	})
	<-jobA.done
	first.Close()

	second := NewManager(event.Discard)
	second.SetActiveSessionPath("session-b", pathB)
	jobB := second.StartForSession("session-b", "bash", "b", func(_ context.Context, out io.Writer) (string, error) {
		_, _ = io.WriteString(out, "from-b")
		return "", nil
	})
	<-jobB.done
	second.Close()

	if jobA.ID != "bash-1" || jobB.ID != "bash-1" {
		t.Fatalf("test setup expected duplicate ids, got %s and %s", jobA.ID, jobB.ID)
	}

	m := NewManager(event.Discard)
	defer m.Close()
	m.SetActiveSessionPath("session-a", pathA)
	m.SetActiveSessionPath("session-b", pathB)

	resA := m.WaitForSession(context.Background(), "session-a", []string{"bash-1"}, 1)
	if len(resA) != 1 || resA[0].Output != "from-a" {
		t.Fatalf("session-a wait = %+v, want from-a", resA)
	}
	resB := m.WaitForSession(context.Background(), "session-b", []string{"bash-1"}, 1)
	if len(resB) != 1 || resB[0].Output != "from-b" {
		t.Fatalf("session-b wait = %+v, want from-b", resB)
	}

	next := m.StartForSession("session-b", "bash", "next", func(context.Context, io.Writer) (string, error) {
		return "", nil
	})
	<-next.done
	if next.ID == "bash-1" {
		t.Fatal("new job should not reuse restored bash-1")
	}
}
