package runjournal

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestJournalContentLimitedAndRepairsTornTail(t *testing.T) {
	path := filepath.Join(t.TempDir(), "run.ndjson")
	j := New()
	if err := j.Bind(path); err != nil {
		t.Fatal(err)
	}
	raw := "/private/work/secret.txt"
	if err := j.Append(Entry{Type: "tool_receipt", Tool: "read", InputDigest: Digest(raw), PathDigests: []string{Digest(raw)}, OutputBytes: 42}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), raw) {
		t.Fatalf("journal leaked raw content: %s", b)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("journal mode = %o, want 600", got)
	}
	if err := os.WriteFile(path, append(b, []byte(`{"schema_version":1`)...), 0o600); err != nil {
		t.Fatal(err)
	}
	j2 := New()
	if err := j2.Bind(path); err != nil {
		t.Fatal(err)
	}
	if err := j2.Append(Entry{Type: "run_finished", Detail: "success"}); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(path)
	if strings.Contains(string(b), `{"schema_version":1{"`) {
		t.Fatalf("torn tail was not repaired: %s", b)
	}
}

func TestJournalRejectsFutureSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "run.ndjson")
	if err := os.WriteFile(path, []byte(`{"schema_version":99,"sequence":1}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := New().Bind(path); err == nil {
		t.Fatal("expected future schema error")
	}
}

func TestJournalRejectsCorruptionBeforeTail(t *testing.T) {
	path := filepath.Join(t.TempDir(), "run.ndjson")
	body := `{"schema_version":1,"sequence":1,"timestamp":"x","type":"ok"}` + "\n" + `{broken}` + "\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := New().Bind(path); err == nil {
		t.Fatal("expected complete-line corruption error")
	}
}
