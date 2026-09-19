package responses

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"

	"reasonix/internal/provider"
)

func responsesSSEEvent(payload string) string {
	return "data: " + payload + "\n\n"
}

func responsesToolCallAdded(itemID, callID, name string) string {
	item, _ := json.Marshal(map[string]string{"id": itemID, "type": "function_call", "call_id": callID, "name": name})
	return responsesSSEEvent(fmt.Sprintf(`{"type":"response.output_item.added","item":%s}`, item))
}

func responsesToolCallDelta(itemID, delta string) string {
	id, _ := json.Marshal(itemID)
	text, _ := json.Marshal(delta)
	return responsesSSEEvent(fmt.Sprintf(`{"type":"response.function_call_arguments.delta","item_id":%s,"delta":%s}`, id, text))
}

func responsesToolCallArgumentsDone(itemID, arguments string) string {
	id, _ := json.Marshal(itemID)
	text, _ := json.Marshal(arguments)
	return responsesSSEEvent(fmt.Sprintf(`{"type":"response.function_call_arguments.done","item_id":%s,"arguments":%s}`, id, text))
}

func responsesToolCallItemDone(itemID, callID, name, arguments string) string {
	item, _ := json.Marshal(map[string]string{"id": itemID, "type": "function_call", "call_id": callID, "name": name, "arguments": arguments})
	return responsesSSEEvent(fmt.Sprintf(`{"type":"response.output_item.done","item":%s}`, item))
}

func responsesCompleted(responseID string) string {
	id, _ := json.Marshal(responseID)
	return responsesSSEEvent(fmt.Sprintf(`{"type":"response.completed","response":{"id":%s}}`, id))
}

func responsesCumulativeProgress(parts []string) []int {
	progress := make([]int, 0, len(parts))
	total := 0
	for _, part := range parts {
		total += len(part)
		progress = append(progress, total)
	}
	return progress
}

func responsesSplitArgument(argument string, size int) []string {
	parts := make([]string, 0, (len(argument)+size-1)/size)
	for len(argument) > 0 {
		n := min(size, len(argument))
		parts = append(parts, argument[:n])
		argument = argument[n:]
	}
	return parts
}

func TestReadStreamPreservesToolArgumentProgressAndOverrides(t *testing.T) {
	cjk := string(rune(0x754c))
	deltaA := []string{`{"draft":"`, strings.Repeat(cjk, 800), strings.Repeat(`\n`, 200), `"}`}
	deltaB := []string{`{"command":"`, strings.Repeat("echo ", 300), `done"}`}
	deltaC := []string{`{"keep":"`, strings.Repeat(cjk, 800), strings.Repeat(`\n`, 200), `"}`}
	functionDoneA := `{"source":"function-done","value":"` + cjk + `"}`
	outputDoneA := `{"source":"output-item","value":"` + cjk + `"}`
	outputDoneB := `{"source":"output-item","command":"pwd"}`
	keptC := strings.Join(deltaC, "")
	for _, argument := range []string{strings.Join(deltaA, ""), strings.Join(deltaB, ""), keptC, functionDoneA, outputDoneA, outputDoneB} {
		if !json.Valid([]byte(argument)) {
			t.Fatalf("invalid JSON test argument: %q", argument)
		}
	}

	var fixture strings.Builder
	fixture.WriteString(responsesToolCallAdded("fc_a", "call_a", "write_file"))
	fixture.WriteString(responsesToolCallAdded("fc_b", "call_b", "bash"))
	fixture.WriteString(responsesToolCallAdded("fc_c", "call_c", "keep"))
	for i := range max(len(deltaA), len(deltaB), len(deltaC)) {
		if i < len(deltaA) {
			fixture.WriteString(responsesToolCallDelta("fc_a", deltaA[i]))
		}
		if i < len(deltaB) {
			fixture.WriteString(responsesToolCallDelta("fc_b", deltaB[i]))
		}
		if i < len(deltaC) {
			fixture.WriteString(responsesToolCallDelta("fc_c", deltaC[i]))
		}
	}
	fixture.WriteString(responsesToolCallArgumentsDone("fc_a", functionDoneA))
	fixture.WriteString(responsesToolCallItemDone("fc_a", "call_a", "write_file", outputDoneA))
	fixture.WriteString(responsesToolCallItemDone("fc_b", "call_b", "bash", outputDoneB))
	fixture.WriteString(responsesToolCallArgumentsDone("fc_c", ""))
	fixture.WriteString(responsesToolCallItemDone("fc_c", "call_c", "keep", ""))
	fixture.WriteString(responsesCompleted("resp_1"))

	requestMessages := []provider.Message{{Role: provider.RoleUser, Content: "run tools"}}
	c := &client{name: "test"}
	out := make(chan provider.Chunk, 64)
	c.readStream(context.Background(), &http.Response{Body: io.NopCloser(strings.NewReader(fixture.String()))}, out, requestMessages)

	var starts, completed []provider.ToolCall
	progress := map[string][]int{}
	done := false
	for chunk := range out {
		switch chunk.Type {
		case provider.ChunkToolCallStart:
			starts = append(starts, *chunk.ToolCall)
		case provider.ChunkToolCallArgsDelta:
			progress[chunk.ToolCall.ID] = append(progress[chunk.ToolCall.ID], chunk.ArgChars)
		case provider.ChunkToolCall:
			completed = append(completed, *chunk.ToolCall)
		case provider.ChunkDone:
			done = true
		case provider.ChunkError:
			t.Fatalf("unexpected stream error: %v", chunk.Err)
		}
	}

	if len(starts) != 3 || starts[0].ID != "call_a" || starts[1].ID != "call_b" || starts[2].ID != "call_c" {
		t.Fatalf("tool starts = %+v", starts)
	}
	if got, want := progress["call_a"], responsesCumulativeProgress(deltaA); !slices.Equal(got, want) {
		t.Fatalf("call_a progress = %v, want %v", got, want)
	}
	if got, want := progress["call_b"], responsesCumulativeProgress(deltaB); !slices.Equal(got, want) {
		t.Fatalf("call_b progress = %v, want %v", got, want)
	}
	if got, want := progress["call_c"], responsesCumulativeProgress(deltaC); !slices.Equal(got, want) {
		t.Fatalf("call_c progress = %v, want %v", got, want)
	}
	if len(completed) != 3 {
		t.Fatalf("completed calls = %d, want 3", len(completed))
	}
	if completed[0].ID != "call_a" || completed[0].Name != "write_file" || completed[0].Arguments != functionDoneA {
		t.Fatalf("function-done call = %+v", completed[0])
	}
	if completed[1].ID != "call_b" || completed[1].Name != "bash" || completed[1].Arguments != outputDoneB {
		t.Fatalf("output-item-done call = %+v", completed[1])
	}
	if completed[2].ID != "call_c" || completed[2].Name != "keep" || completed[2].Arguments != keptC {
		t.Fatalf("empty-done call = %+v", completed[2])
	}
	if !done || c.lastResponseID != "resp_1" {
		t.Fatalf("done=%v lastResponseID=%q", done, c.lastResponseID)
	}
	wantAssistant := provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{
		{ID: "call_a", Name: "write_file", Arguments: outputDoneA},
		{ID: "call_b", Name: "bash", Arguments: outputDoneB},
		{ID: "call_c", Name: "keep", Arguments: keptC},
	}}
	wantDigest := c.conversationDigest(append(append([]provider.Message(nil), requestMessages...), wantAssistant))
	if c.expectedPrefixDigest != wantDigest {
		t.Fatalf("state digest used stale arguments: got %q want %q", c.expectedPrefixDigest, wantDigest)
	}
	staleAssistant := wantAssistant
	staleAssistant.ToolCalls = append([]provider.ToolCall(nil), wantAssistant.ToolCalls...)
	staleAssistant.ToolCalls[0].Arguments = functionDoneA
	staleDigest := c.conversationDigest(append(append([]provider.Message(nil), requestMessages...), staleAssistant))
	if wantDigest == staleDigest {
		t.Fatal("digest fixture cannot distinguish authoritative argument override")
	}
}

func TestReadStreamDoesNotCompletePartialLargeToolArguments(t *testing.T) {
	partial := strings.Repeat("x", 2048)
	fixture := responsesToolCallAdded("fc_1", "call_1", "write_file") +
		responsesToolCallDelta("fc_1", partial)

	c := &client{name: "test"}
	out := make(chan provider.Chunk, 8)
	c.readStream(context.Background(), &http.Response{Body: io.NopCloser(strings.NewReader(fixture))}, out, nil)

	var started, progressed, interrupted bool
	for chunk := range out {
		switch chunk.Type {
		case provider.ChunkToolCallStart:
			started = chunk.ToolCall != nil && chunk.ToolCall.ID == "call_1" && chunk.ToolCall.Name == "write_file"
		case provider.ChunkToolCallArgsDelta:
			progressed = chunk.ToolCall != nil && chunk.ToolCall.ID == "call_1" && chunk.ArgChars == len(partial)
		case provider.ChunkError:
			var streamErr *provider.StreamInterruptedError
			interrupted = errors.As(chunk.Err, &streamErr)
		case provider.ChunkToolCall, provider.ChunkDone:
			t.Fatalf("partial stream surfaced terminal chunk: %+v", chunk)
		}
	}
	if !started || !progressed || !interrupted {
		t.Fatalf("started=%v progressed=%v interrupted=%v, want all partial-stream signals", started, progressed, interrupted)
	}
}

func BenchmarkReadStreamToolArguments(b *testing.B) {
	cases := []struct {
		name            string
		total, fragment int
	}{
		{name: "64B/one-fragment", total: 64, fragment: 64},
		{name: "512B/128B-fragments", total: 512, fragment: 128},
		{name: "2KiB/128B-fragments", total: 2 << 10, fragment: 128},
		{name: "4KiB/128B-fragments", total: 4 << 10, fragment: 128},
		{name: "32KiB/128B-fragments", total: 32 << 10, fragment: 128},
		{name: "128KiB/128B-fragments", total: 128 << 10, fragment: 128},
	}
	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			prefix, suffix := `{"content":"`, `"}`
			argument := prefix + strings.Repeat("x", tc.total-len(prefix)-len(suffix)) + suffix
			fragments := responsesSplitArgument(argument, tc.fragment)
			var fixture strings.Builder
			fixture.WriteString(responsesToolCallAdded("fc_1", "call_1", "write_file"))
			for _, fragment := range fragments {
				fixture.WriteString(responsesToolCallDelta("fc_1", fragment))
			}
			fixture.WriteString(responsesToolCallArgumentsDone("fc_1", argument))
			fixture.WriteString(responsesToolCallItemDone("fc_1", "call_1", "write_file", argument))
			fixture.WriteString(responsesCompleted("resp_1"))
			c := &client{name: "benchmark"}
			var got string

			b.ReportAllocs()
			b.SetBytes(int64(len(argument)))
			for b.Loop() {
				out := make(chan provider.Chunk, len(fragments)+8)
				c.readStream(context.Background(), &http.Response{Body: io.NopCloser(strings.NewReader(fixture.String()))}, out, nil)
				for chunk := range out {
					if chunk.Type == provider.ChunkToolCall {
						got = chunk.ToolCall.Arguments
					}
				}
			}
			if got != argument {
				b.Fatalf("completed arguments have %d bytes, want %d", len(got), len(argument))
			}
		})
	}
}
