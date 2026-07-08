package doctor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/provider"
	"reasonix/internal/store"
)

func TestRedactSessionsScrubsHistoricalSessionArtifacts(t *testing.T) {
	dir := t.TempDir()
	const secret = "sk-real-secret-value-123456"
	sessionPath := filepath.Join(dir, "abc.jsonl")
	files := map[string]string{
		sessionPath:                                                     `{"role":"tool","content":"DEEPSEEK_API_KEY=` + secret + `"}` + "\n",
		store.SessionEventLog(sessionPath):                              `{"schema_version":1,"type":"replace","messages":[{"role":"tool","content":"DEEPSEEK_API_KEY=` + secret + `"}]}` + "\n",
		store.SessionMeta(sessionPath):                                  `{"id":"abc","preview":"DEEPSEEK_API_KEY=` + secret + `"}` + "\n",
		store.SessionGoalState(sessionPath):                             `{"goal":"rotate token ` + secret + `"}` + "\n",
		filepath.Join(store.SessionJobsDir(sessionPath), "bash-1.log"):  "DEEPSEEK_API_KEY=" + secret + "\n",
		filepath.Join(store.SessionJobsDir(sessionPath), "bash-1.json"): `{"label":"echo DEEPSEEK_API_KEY=` + secret + `"}` + "\n",
		store.SessionEventIndex(sessionPath):                            `{"schema_version":1}` + "\n",
	}
	for path, body := range files {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	res := RedactSessions(RedactSessionsOptions{Dirs: []string{dir}})
	if len(res.Errors) > 0 {
		t.Fatalf("RedactSessions errors = %v", res.Errors)
	}
	if res.FilesChanged != 6 {
		t.Fatalf("FilesChanged = %d, want 6", res.FilesChanged)
	}
	for path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if strings.Contains(string(data), secret) {
			t.Fatalf("%s still leaked secret:\n%s", path, data)
		}
	}
	// The rewrite must go through the real save machinery: the session still
	// loads, the event log still replays, and the masked value survived.
	loaded, err := agent.LoadSession(sessionPath)
	if err != nil {
		t.Fatalf("redacted session no longer loads: %v", err)
	}
	if len(loaded.Messages) != 1 || !strings.Contains(loaded.Messages[0].Content, "DEEPSEEK_API_KEY=sk-rea") {
		t.Fatalf("redacted session lost its masked content: %+v", loaded.Messages)
	}
}

// TestRedactSessionsHandlesQuotedSecretsWithoutCorruption pins the decode-
// before-redact contract: on disk a quoted secret is JSON-encoded with \"
// escapes, and masking the raw bytes would eat the escape's backslash,
// truncate the JSON string, and leave the transcript undecodable — while the
// secret itself stayed in the clear.
func TestRedactSessionsHandlesQuotedSecretsWithoutCorruption(t *testing.T) {
	dir := t.TempDir()
	const secret = "hunter2-longer-secret-value"
	sessionPath := filepath.Join(dir, "abc.jsonl")
	line, err := json.Marshal(provider.Message{
		Role:    provider.RoleTool,
		Content: `export PASSWORD="` + secret + `"` + "\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sessionPath, append(line, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}

	res := RedactSessions(RedactSessionsOptions{Dirs: []string{dir}})
	if len(res.Errors) > 0 {
		t.Fatalf("RedactSessions errors = %v", res.Errors)
	}
	if res.FilesChanged != 1 {
		t.Fatalf("FilesChanged = %d, want 1", res.FilesChanged)
	}
	loaded, err := agent.LoadSession(sessionPath)
	if err != nil {
		t.Fatalf("redaction corrupted the transcript: %v", err)
	}
	if len(loaded.Messages) != 1 {
		t.Fatalf("message count = %d, want 1", len(loaded.Messages))
	}
	if strings.Contains(loaded.Messages[0].Content, secret) {
		t.Fatalf("quoted secret leaked: %q", loaded.Messages[0].Content)
	}
}

// TestRedactSessionsIsNoOpOnHealthyStore pins idempotence: a store written by
// a redacting build must survive the doctor untouched — rerunning cleanup on
// clean sessions must never rewrite (or worse, corrupt) them.
func TestRedactSessionsIsNoOpOnHealthyStore(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "abc.jsonl")
	s := agent.NewSession("sys")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "inspect"})
	s.Add(provider.Message{
		Role:       provider.RoleTool,
		Name:       "bash",
		ToolCallID: "call_1",
		Content:    `export PASSWORD="hunter2-longer-secret-value"` + "\n",
	})
	if err := s.Save(sessionPath); err != nil {
		t.Fatalf("Save: %v", err)
	}
	before, err := os.ReadFile(sessionPath)
	if err != nil {
		t.Fatal(err)
	}

	res := RedactSessions(RedactSessionsOptions{Dirs: []string{dir}})
	if len(res.Errors) > 0 {
		t.Fatalf("RedactSessions errors = %v", res.Errors)
	}
	if res.FilesChanged != 0 {
		t.Fatalf("healthy already-redacted store rewritten: %+v", res)
	}
	after, err := os.ReadFile(sessionPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatalf("healthy transcript bytes changed:\nbefore: %s\nafter:  %s", before, after)
	}
	if _, err := agent.LoadSession(sessionPath); err != nil {
		t.Fatalf("healthy session no longer loads: %v", err)
	}
}

func TestRedactSessionsDryRunDoesNotWrite(t *testing.T) {
	dir := t.TempDir()
	const secret = "sk-real-secret-value-123456"
	path := filepath.Join(dir, "abc.jsonl")
	body := `{"role":"tool","content":"DEEPSEEK_API_KEY=` + secret + `"}` + "\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	res := RedactSessions(RedactSessionsOptions{Dirs: []string{dir}, DryRun: true})
	if res.FilesChanged != 1 {
		t.Fatalf("FilesChanged = %d, want 1", res.FilesChanged)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != body {
		t.Fatalf("dry-run modified file:\n%s", data)
	}
}

func TestRedactSessionsSkipsLeasedSession(t *testing.T) {
	dir := t.TempDir()
	const secret = "sk-real-secret-value-123456"
	path := filepath.Join(dir, "abc.jsonl")
	if err := os.WriteFile(path, []byte(`{"role":"tool","content":"DEEPSEEK_API_KEY=`+secret+`"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	lease, err := agent.TryAcquireSessionLease(path)
	if err != nil {
		t.Fatalf("TryAcquireSessionLease: %v", err)
	}
	defer lease.Release()

	res := RedactSessions(RedactSessionsOptions{Dirs: []string{dir}})
	if res.FilesSkipped != 1 {
		t.Fatalf("FilesSkipped = %d, want 1", res.FilesSkipped)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), secret) {
		t.Fatalf("leased session should not be rewritten:\n%s", data)
	}
}

// TestRedactSessionsScrubsStaleEventLogRecords pins the stale-record gap: a
// later replace event supersedes — but does not erase — earlier records, so a
// raw key can survive in an old event while the replayed view is already
// clean. Cleanup must compact the log anyway, and the replayed transcript
// (the clean current view) must be what survives.
func TestRedactSessionsScrubsStaleEventLogRecords(t *testing.T) {
	dir := t.TempDir()
	const secret = "sk-real-secret-value-123456"
	sessionPath := filepath.Join(dir, "abc.jsonl")
	events := `{"schema_version":1,"type":"replace","messages":[{"role":"tool","content":"DEEPSEEK_API_KEY=` + secret + `"}]}` + "\n" +
		`{"schema_version":1,"type":"replace","messages":[{"role":"user","content":"clean"}]}` + "\n"
	if err := os.WriteFile(sessionPath, []byte(`{"role":"user","content":"clean"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	evPath := store.SessionEventLog(sessionPath)
	if err := os.WriteFile(evPath, []byte(events), 0o644); err != nil {
		t.Fatal(err)
	}

	res := RedactSessions(RedactSessionsOptions{Dirs: []string{dir}})
	if len(res.Errors) > 0 {
		t.Fatalf("RedactSessions errors = %v", res.Errors)
	}
	if res.FilesChanged != 2 {
		t.Fatalf("FilesChanged = %d, want 2 (anchor + event log)", res.FilesChanged)
	}
	data, err := os.ReadFile(evPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), secret) {
		t.Fatalf("stale event log record still leaks secret:\n%s", data)
	}
	loaded, err := agent.LoadSession(sessionPath)
	if err != nil {
		t.Fatalf("session no longer loads after compaction: %v", err)
	}
	if len(loaded.Messages) != 1 || loaded.Messages[0].Content != "clean" {
		t.Fatalf("compaction lost the current replayed view: %+v", loaded.Messages)
	}
}
