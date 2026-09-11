package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"

	"reasonix/internal/taskmonitor"
)

func captureErr(t *testing.T, fn func() int) (int, string) {
	t.Helper()
	old := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w
	defer func() { os.Stderr = old }()

	ec := fn()
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	return ec, string(data)
}

func TestUnknownOperationNamesTheValidOnes(t *testing.T) {
	for _, tc := range []struct {
		name string
		run  func(args []string, out *bytes.Buffer) int
		want []string
	}{
		{
			name: "hook",
			run:  func(a []string, o *bytes.Buffer) int { return runHookCommand(a, o) },
			want: []string{"list", "status"},
		},
		{
			name: "session",
			run:  func(a []string, o *bytes.Buffer) int { return runSessionCommand(a, o) },
			want: []string{"list", "show", "status", "recovery"},
		},
		{
			name: "task",
			run:  func(a []string, o *bytes.Buffer) int { return runTaskCommand(a, o) },
			want: []string{"list", "show"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			if rc := tc.run([]string{"bogus", "--json"}, &out); rc != 2 {
				t.Fatalf("exit code = %d, want 2", rc)
			}
			var payload struct {
				Error struct {
					Code    string `json:"code"`
					Message string `json:"message"`
				} `json:"error"`
			}
			if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
				t.Fatalf("machine output is not JSON: %v (%s)", err, out.String())
			}
			if payload.Error.Code != "unknown_command" {
				t.Fatalf("code = %q, want unknown_command", payload.Error.Code)
			}
			if !strings.Contains(payload.Error.Message, "bogus") {
				t.Fatalf("message does not echo the rejected operation: %s", payload.Error.Message)
			}
			for _, op := range tc.want {
				if !strings.Contains(payload.Error.Message, op) {
					t.Fatalf("message omits valid operation %q: %s", op, payload.Error.Message)
				}
			}
		})
	}
}

func TestTaskUnknownSubcommandsNameValidOnes(t *testing.T) {
	store := taskmonitor.NewInMemoryStore()
	originalStore := taskStore
	taskStore = store
	t.Cleanup(func() { taskStore = originalStore })

	for _, tc := range []struct {
		name      string
		run       func() int
		wantUsage string
	}{
		{
			name:      "task",
			run:       func() int { return taskCommand([]string{"bogus"}) },
			wantUsage: "usage: reasonix task <list|show|monitor|status|events|stop|cancel|requeue|open-session|tmux> [flags]",
		},
		{
			name:      "task monitor",
			run:       func() int { return taskMonitorCommand(store, []string{"bogus"}) },
			wantUsage: "usage: reasonix task monitor <list|status|events|stop|cancel|requeue|open-session> [flags]",
		},
		{
			name:      "task tmux",
			run:       func() int { return taskTmuxCmd(store, []string{"bogus"}) },
			wantUsage: "usage: reasonix task tmux <attach|status|open|detach>",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			exit, stderr := captureErr(t, tc.run)
			if exit != 2 {
				t.Fatalf("exit = %d, want 2", exit)
			}
			if !strings.Contains(stderr, "bogus") {
				t.Fatalf("stderr does not echo the rejected subcommand: %s", stderr)
			}
			if !strings.Contains(stderr, tc.wantUsage) {
				t.Fatalf("stderr does not include usage %q: %s", tc.wantUsage, stderr)
			}
		})
	}
}
