package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"reasonix/internal/event"
	"reasonix/internal/extension"
	"reasonix/internal/extension/dispatch"
	"reasonix/internal/extension/protocol"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

func TestToolBeforeWorkspaceMutationUsesExecutedReplacement(t *testing.T) {
	t.Run("reader replaced by writer", func(t *testing.T) {
		client := &fakeDispatchClient{interceptFn: func(ev protocol.InterceptEvent, _ json.RawMessage) (protocol.InterceptResult, error) {
			if ev == protocol.EventToolBefore {
				return replaceWith(t, dispatch.ToolBeforePayload{Name: "write_file", Arguments: `{"path":"effective.go","content":"x"}`}), nil
			}
			return protocol.InterceptResult{Decision: protocol.DecisionContinue}, nil
		}}
		reg := tool.NewRegistry()
		reg.Add(&recordingTool{name: "read_file", readOnly: true})
		reg.Add(&recordingTool{name: "write_file", readOnly: false})
		sink := newWorkspaceSignalSink()
		a := New(nil, reg, NewSession(""), Options{Extensions: newExtDispatcher(client, true, nil, extension.PointToolBefore)}, sink)
		a.executeBatch(context.Background(), &a.turn, []provider.ToolCall{{ID: "call", Name: "read_file", Arguments: `{"path":"original.go"}`}})

		select {
		case mutation := <-sink.mutations:
			if mutation.ToolName != "write_file" || len(mutation.Paths) != 1 || mutation.Paths[0] != "effective.go" {
				t.Fatalf("replacement workspace mutation = %+v", mutation)
			}
		default:
			t.Fatal("executed writer replacement did not publish a workspace mutation")
		}
		results := sink.kinds(event.ToolResult)
		if len(results) != 1 || !results[0].Tool.WorkspaceMutation || len(results[0].Tool.WorkspacePaths) != 1 || results[0].Tool.WorkspacePaths[0] != "effective.go" {
			t.Fatalf("replacement ToolResult metadata = %+v", results)
		}
	})

	t.Run("writer replaced by reader", func(t *testing.T) {
		client := &fakeDispatchClient{interceptFn: func(ev protocol.InterceptEvent, _ json.RawMessage) (protocol.InterceptResult, error) {
			if ev == protocol.EventToolBefore {
				return replaceWith(t, dispatch.ToolBeforePayload{Name: "read_file", Arguments: `{"path":"effective.go"}`}), nil
			}
			return protocol.InterceptResult{Decision: protocol.DecisionContinue}, nil
		}}
		reg := tool.NewRegistry()
		reg.Add(&recordingTool{name: "write_file", readOnly: false})
		reg.Add(&recordingTool{name: "read_file", readOnly: true})
		sink := newWorkspaceSignalSink()
		a := New(nil, reg, NewSession(""), Options{Extensions: newExtDispatcher(client, true, nil, extension.PointToolBefore)}, sink)
		a.executeBatch(context.Background(), &a.turn, []provider.ToolCall{{ID: "call", Name: "write_file", Arguments: `{"path":"original.go","content":"x"}`}})

		select {
		case mutation := <-sink.mutations:
			t.Fatalf("reader replacement published a false workspace mutation: %+v", mutation)
		default:
		}
		results := sink.kinds(event.ToolResult)
		if len(results) != 1 || results[0].Tool.WorkspaceMutation {
			t.Fatalf("reader replacement ToolResult metadata = %+v", results)
		}
	})
}

func TestToolBeforeWriterReplacementSignalsBeforeParallelPeerCompletes(t *testing.T) {
	client := &fakeDispatchClient{interceptFn: func(ev protocol.InterceptEvent, payload json.RawMessage) (protocol.InterceptResult, error) {
		if ev != protocol.EventToolBefore {
			return protocol.InterceptResult{Decision: protocol.DecisionContinue}, nil
		}
		var call dispatch.ToolBeforePayload
		if err := json.Unmarshal(payload, &call); err != nil {
			return protocol.InterceptResult{}, err
		}
		if call.Name == "read_file" {
			return replaceWith(t, dispatch.ToolBeforePayload{Name: "write_file", Arguments: `{"path":"effective.go","content":"x"}`}), nil
		}
		return protocol.InterceptResult{Decision: protocol.DecisionContinue}, nil
	}}
	started := make(chan struct{})
	release := make(chan struct{})
	reg := tool.NewRegistry()
	reg.Add(&recordingTool{name: "read_file", readOnly: true})
	reg.Add(&recordingTool{name: "write_file", readOnly: false})
	reg.Add(blockingTool{name: "slow_read", started: started, release: release})
	sink := newWorkspaceSignalSink()
	a := New(nil, reg, NewSession(""), Options{Extensions: newExtDispatcher(client, true, nil, extension.PointToolBefore)}, sink)
	done := make(chan struct{})
	go func() {
		defer close(done)
		a.executeBatch(context.Background(), &a.turn, []provider.ToolCall{
			{ID: "writer", Name: "read_file", Arguments: `{"path":"original.go"}`},
			{ID: "reader", Name: "slow_read", Arguments: `{}`},
		})
	}()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("parallel read peer did not start")
	}
	select {
	case mutation := <-sink.mutations:
		if mutation.ToolName != "write_file" || len(mutation.Paths) != 1 || mutation.Paths[0] != "effective.go" {
			t.Fatalf("replacement workspace mutation = %+v", mutation)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("writer replacement waited for its parallel peer")
	}
	close(release)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("parallel batch did not finish after releasing the peer")
	}
}

func TestToolBeforeFailedWriterReplacementOpensDependencyBarrier(t *testing.T) {
	client := &fakeDispatchClient{interceptFn: func(ev protocol.InterceptEvent, payload json.RawMessage) (protocol.InterceptResult, error) {
		if ev != protocol.EventToolBefore {
			return protocol.InterceptResult{Decision: protocol.DecisionContinue}, nil
		}
		var call dispatch.ToolBeforePayload
		if err := json.Unmarshal(payload, &call); err != nil {
			return protocol.InterceptResult{}, err
		}
		if call.Name == "read_file" {
			return replaceWith(t, dispatch.ToolBeforePayload{Name: "write_one", Arguments: `{"path":"first.go"}`}), nil
		}
		return protocol.InterceptResult{Decision: protocol.DecisionContinue}, nil
	}}
	var secondCalls int32
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "read_file", readOnly: true})
	reg.Add(fakeTool{name: "write_one", err: errors.New("partial write")})
	reg.Add(fakeTool{name: "write_two", calls: &secondCalls})
	a := New(nil, reg, NewSession(""), Options{Extensions: newExtDispatcher(client, true, nil, extension.PointToolBefore)}, event.Discard)
	batch := a.executeBatch(context.Background(), &a.turn, []provider.ToolCall{
		{ID: "first", Name: "read_file", Arguments: `{"path":"original.go"}`},
		{ID: "second", Name: "write_two", Arguments: `{"path":"second.go"}`},
	})
	if secondCalls != 0 {
		t.Fatalf("later writer executed %d times after the replaced writer failed", secondCalls)
	}
	if len(batch.results) != 2 || !strings.Contains(batch.results[1], "skipped because an earlier modification") {
		t.Fatalf("dependency results = %+v", batch.results)
	}
}

func TestSubSinkForwardsWorkspaceMutationToParent(t *testing.T) {
	parent := newWorkspaceSignalSink()
	event.RecordWorkspaceMutation(subSinkFor("task_1", parent), event.WorkspaceMutation{
		ToolID: "write", ToolName: "write_file", Paths: []string{"child.go"}, Content: true,
	})
	select {
	case mutation := <-parent.mutations:
		if mutation.ToolName != "write_file" || len(mutation.Paths) != 1 || mutation.Paths[0] != "child.go" {
			t.Fatalf("forwarded workspace mutation = %+v", mutation)
		}
	default:
		t.Fatal("sub-agent workspace mutation was not forwarded")
	}
}
