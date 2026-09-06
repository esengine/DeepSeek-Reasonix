package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/agent/testutil"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
	"reasonix/internal/tool/builtin"
)

func TestPreparedToolContextPreservesDurableWriteIntentHook(t *testing.T) {
	for _, fail := range []bool{false, true} {
		name := "checkpoint_success"
		if fail {
			name = "checkpoint_failure"
		}
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			target := filepath.Join(root, "written.txt")
			reg := tool.NewRegistry()
			for _, w := range builtin.ConfineWriters([]string{root}, builtin.SessionDataGuard{}, builtin.ManagedConfigPaths{}) {
				if w.Name() == "write_file" {
					reg.Add(w)
				}
			}
			args, _ := json.Marshal(map[string]string{"path": target, "content": "durable marker"})
			p := testutil.NewMock("fixture", testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "write", Name: "write_file", Arguments: string(args)}}}, testutil.Turn{Text: "done"})
			sess := NewSession("")
			sink := &intentDispatchSink{recordSink: &recordSink{}, target: target, fail: fail}
			a := New(p, reg, sess, Options{}, sink)
			_ = a.Run(withNoClosedLoop(context.Background()), "write the marker")
			if sink.intents != 1 {
				t.Fatalf("intent hooks=%d", sink.intents)
			}
			data, err := os.ReadFile(target)
			if fail {
				if !os.IsNotExist(err) {
					t.Fatalf("checkpoint failure still wrote file: err=%v", err)
				}
			} else if err != nil || string(data) != "durable marker" {
				t.Fatalf("write failed: %v", err)
			}
			intents := 0
			for _, m := range sess.Snapshot() {
				for _, call := range m.ToolCalls {
					intents += len(call.WriteIntents)
				}
			}
			if intents != 1 {
				t.Fatalf("recorded intents=%d", intents)
			}
		})
	}
}

type intentDispatchSink struct {
	*recordSink
	target  string
	fail    bool
	intents int
}

func (s *intentDispatchSink) EmitChecked(e event.Event) error {
	if e.WriteIntent {
		s.intents++
		if _, err := os.Stat(s.target); !os.IsNotExist(err) {
			return errors.New("write started before checkpoint")
		}
		if s.fail {
			return errors.New("injected intent persistence failure")
		}
	}
	s.Emit(e)
	return nil
}
