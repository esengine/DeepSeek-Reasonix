package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/eventwire"
)

// testdata/v1-run holds `reasonix run` v1.39.5 output for one tool call and an
// answer. A run here keeps its lifecycle order, per-line envelope, and kind set.

var runLifecycleKinds = []string{"turn_status", "turn_started", "user_message", "tool_started", "tool_result", "turn_done"}

type streamRecord map[string]any

func (r streamRecord) kind() string { s, _ := r["kind"].(string); return s }

func readStreamRecords(t *testing.T, text string) []streamRecord {
	t.Helper()
	var out []streamRecord
	sc := bufio.NewScanner(strings.NewReader(text))
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var rec streamRecord
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("line is not JSON: %v\n%s", err, line)
		}
		out = append(out, rec)
	}
	return out
}

func readV1Sample(t *testing.T, name string) []streamRecord {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "v1-run", name))
	if err != nil {
		t.Fatal(err)
	}
	return readStreamRecords(t, string(data))
}

func lifecycleSkeleton(records []streamRecord) []string {
	var out []string
	for _, r := range records {
		if slices.Contains(runLifecycleKinds, r.kind()) {
			out = append(out, r.kind())
		}
	}
	return out
}

func streamVocabulary() map[string]bool {
	names := map[string]bool{"result": true, "run_done": true}
	for _, k := range runLifecycleKinds {
		names[k] = true
	}
	for kind := range runStreamKinds {
		name, _ := eventwire.KindName(kind)
		names[name] = true
	}
	return names
}

// runToolCallFixture answers the first request with one call to name(args) and
// every later one with "done".
func runToolCallFixture(t *testing.T, name, args string) string {
	t.Helper()
	var mu sync.Mutex
	turn := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		mu.Lock()
		turn++
		n := turn
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		if n == 1 {
			call, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"index": 0, "delta": map[string]any{
				"tool_calls": []any{map[string]any{"index": 0, "id": "call_1", "type": "function",
					"function": map[string]any{"name": name, "arguments": args}}},
			}}}})
			_, _ = fmt.Fprintf(w, "data: %s\n\n", call)
			_, _ = io.WriteString(w, `data: {"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12}}`+"\n\ndata: [DONE]\n\n")
			return
		}
		_, _ = io.WriteString(w, `data: {"choices":[{"index":0,"delta":{"content":"done"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12}}`+"\n\ndata: [DONE]\n\n")
	}))
	t.Cleanup(srv.Close)
	isolateCLIConfigHome(t)
	home := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("RUN_FLAGS_FAKE_KEY", "k")
	writeTestFile(t, filepath.Join(home, "config.toml"), fmt.Sprintf(`default_model = "fake"

[[providers]]
name = "fake"
kind = "openai"
base_url = %q
model = "fake-model"
api_key_env = "RUN_FLAGS_FAKE_KEY"
`, srv.URL), 0o644)
	dir := testenv.TempDir(t)
	t.Chdir(dir)
	writeTestFile(t, filepath.Join(dir, "note.txt"), "hello\n", 0o644)
	return dir
}

func runCLIStream(t *testing.T, argv ...string) []streamRecord {
	t.Helper()
	var code int
	stdout, stderr := captureDrainedOutput(t, func() { code = Run(argv, "test") })
	if code != 0 && code != runExitUntrustedFolder {
		t.Fatalf("Run(%q) exited %d\nstdout:\n%s\nstderr:\n%s", argv, code, stdout, stderr)
	}
	return readStreamRecords(t, stdout)
}

func TestRunStreamJSONKeeps1xLineContract(t *testing.T) {
	v1 := readV1Sample(t, "stream-json-write-file.jsonl")
	runToolCallFixture(t, "read_file", `{"path":"note.txt"}`)
	got := runCLIStream(t, "run", "--output-format", "stream-json", "READ note.txt")

	if want, have := lifecycleSkeleton(v1), lifecycleSkeleton(got); !slices.Equal(want, have) {
		t.Fatalf("lifecycle records = %v, want 1.x's %v", have, want)
	}
	if last := got[len(got)-1]; last.kind() != "" || last["type"] != "result" || last["is_error"] != false || last["num_turns"] != float64(1) {
		t.Fatalf("last line = %v, want a successful result with num_turns 1", last)
	}
	events := got[:len(got)-1]
	vocab := streamVocabulary()
	var session, turn string
	for i, r := range events {
		for _, key := range []string{"sessionId", "turnId", "seq", "status"} {
			if _, ok := v1[0][key]; ok && r[key] == nil {
				t.Fatalf("line %d (%s) lacks 1.x envelope key %q: %v", i, r.kind(), key, r)
			}
		}
		if r["seq"] != float64(i+1) {
			t.Fatalf("line %d seq = %v, want %d", i, r["seq"], i+1)
		}
		if i == 0 {
			session, _ = r["sessionId"].(string)
			turn, _ = r["turnId"].(string)
		}
		if r["sessionId"] != session || r["turnId"] != turn || session == "" || !strings.HasPrefix(turn, "turn_") {
			t.Fatalf("line %d envelope = %v/%v, want one session and one turn_ id", i, r["sessionId"], r["turnId"])
		}
		if !vocab[r.kind()] {
			t.Fatalf("line %d has kind %q, which 1.x never emitted", i, r.kind())
		}
	}
	for _, r := range v1 {
		if r.kind() == "" {
			continue
		}
		if !slices.ContainsFunc(events, func(g streamRecord) bool { return g.kind() == r.kind() }) {
			t.Fatalf("1.x emitted %q for this run; the stream here has none", r.kind())
		}
	}
	wantStatus := map[string]string{"turn_status": "queued", "turn_done": "completed", "turn_started": "in_progress", "tool_started": "in_progress"}
	for _, r := range events {
		if want, ok := wantStatus[r.kind()]; ok && r["status"] != want {
			t.Fatalf("%s status = %v, want %s", r.kind(), r["status"], want)
		}
		if r.kind() == "user_message" && r["text"] != "READ note.txt" {
			t.Fatalf("user_message text = %v, want the prompt", r["text"])
		}
	}
	if session != got[len(got)-1]["session_id"] {
		t.Fatalf("envelope sessionId %q differs from the result's session_id %v", session, got[len(got)-1]["session_id"])
	}
}

func TestRunEventsJSONLKeeps1xLifecycle(t *testing.T) {
	v1 := readV1Sample(t, "events-jsonl-write-file.jsonl")
	runToolCallFixture(t, "read_file", `{"path":"note.txt"}`)
	got := runCLIStream(t, "run", "--events-jsonl", "READ note.txt")

	if want, have := lifecycleSkeleton(v1), lifecycleSkeleton(got); !slices.Equal(want, have) {
		t.Fatalf("lifecycle records = %v, want 1.x's %v", have, want)
	}
	vocab := streamVocabulary()
	for i, r := range got {
		if r["sequence"] != float64(i+1) || r["schema_version"] != float64(machineSchemaVersion) {
			t.Fatalf("line %d envelope = %v", i, r)
		}
		if !vocab[r.kind()] {
			t.Fatalf("line %d has kind %q, which 1.x never emitted", i, r.kind())
		}
		if r.kind() == "user_message" && len(r) != 3 {
			t.Fatalf("events-jsonl user_message must stay content-free: %v", r)
		}
	}
	if last := got[len(got)-1]; last.kind() != "run_done" || last["ok"] != true || last["num_turns"] != float64(1) {
		t.Fatalf("last line = %v, want run_done ok with num_turns 1", last)
	}
}

// A call the permission gate refuses never ran, so it has a result and no
// tool_started.
func TestRunStreamJSONReportsNoStartForARefusedCall(t *testing.T) {
	dir := runToolCallFixture(t, "write_file", `{"path":"marker.txt","content":"x"}`)
	got := runCLIStream(t, "run", "--output-format", "stream-json", "WRITE marker.txt")
	if _, err := os.Stat(filepath.Join(dir, "marker.txt")); err == nil {
		t.Fatal("the default headless mode wrote the file; this case needs a refused call")
	}
	skeleton := lifecycleSkeleton(got)
	if slices.Contains(skeleton, "tool_started") || !slices.Contains(skeleton, "tool_result") {
		t.Fatalf("refused call lifecycle = %v, want a tool_result and no tool_started", skeleton)
	}
}

// A turn the provider fails still ran: 1.x counted it and closed it with
// turn_done, status failed.
func TestRunStreamJSONClosesAFailedTurn(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		http.Error(w, `{"error":{"message":"bad request"}}`, http.StatusBadRequest)
	}))
	t.Cleanup(srv.Close)
	isolateCLIConfigHome(t)
	home := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("RUN_FLAGS_FAKE_KEY", "k")
	writeTestFile(t, filepath.Join(home, "config.toml"), fmt.Sprintf(`default_model = "fake"

[[providers]]
name = "fake"
kind = "openai"
base_url = %q
model = "fake-model"
api_key_env = "RUN_FLAGS_FAKE_KEY"
`, srv.URL), 0o644)
	t.Chdir(testenv.TempDir(t))
	var code int
	stdout, stderr := captureDrainedOutput(t, func() { code = Run([]string{"run", "--output-format", "stream-json", "hi"}, "test") })
	if code != 1 {
		t.Fatalf("exit %d, want 1\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	got := readStreamRecords(t, stdout)
	done := got[len(got)-2]
	if done.kind() != "turn_done" || done["status"] != "failed" {
		t.Fatalf("second-to-last line = %v, want turn_done failed", done)
	}
	if result := got[len(got)-1]; result["is_error"] != true || result["num_turns"] != float64(1) {
		t.Fatalf("result = %v, want is_error with num_turns 1", result)
	}
}

// captureDrainedOutput reads both streams while fn runs: a stream-json run
// outgrows a Windows pipe buffer, and an unread pipe blocks the writer.
func captureDrainedOutput(t *testing.T, fn func()) (stdout, stderr string) {
	t.Helper()
	drain := func(target **os.File) func() string {
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		*target = w
		done := make(chan string, 1)
		go func() {
			data, _ := io.ReadAll(r)
			done <- string(data)
		}()
		return func() string { _ = w.Close(); return <-done }
	}
	oldOut, oldErr := os.Stdout, os.Stderr
	defer func() { os.Stdout, os.Stderr = oldOut, oldErr }()
	outDone := drain(&os.Stdout)
	errDone := drain(&os.Stderr)
	fn()
	return outDone(), errDone()
}
