package anthropic

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"

	"reasonix/internal/provider"
)

type anthropicToolArgumentPart struct {
	index          int
	id, name, text string
	start, stop    bool
}

func anthropicToolArgumentStream(parts []anthropicToolArgumentPart, complete bool) string {
	var stream strings.Builder
	for _, part := range parts {
		if part.start {
			id, _ := json.Marshal(part.id)
			name, _ := json.Marshal(part.name)
			fmt.Fprintf(&stream, "data: {\"type\":\"content_block_start\",\"index\":%d,\"content_block\":{\"type\":\"tool_use\",\"id\":%s,\"name\":%s}}\n\n", part.index, id, name)
		}
		if part.text != "" {
			text, _ := json.Marshal(part.text)
			fmt.Fprintf(&stream, "data: {\"type\":\"content_block_delta\",\"index\":%d,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":%s}}\n\n", part.index, text)
		}
		if part.stop {
			fmt.Fprintf(&stream, "data: {\"type\":\"content_block_stop\",\"index\":%d}\n\n", part.index)
		}
	}
	if complete {
		stream.WriteString("data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"tool_use\"}}\n\n")
		stream.WriteString("data: {\"type\":\"message_stop\"}\n\n")
	}
	return stream.String()
}

func anthropicExpectedBucketProgress(parts []string) []int {
	var progress []int
	total, lastBucket := 0, 0
	for _, part := range parts {
		total += len(part)
		if bucket := total / 2048; bucket > lastBucket {
			progress = append(progress, total)
			lastBucket = bucket
		}
	}
	return progress
}

func anthropicSplitArgument(argument string, size int) []string {
	parts := make([]string, 0, (len(argument)+size-1)/size)
	for len(argument) > 0 {
		n := min(size, len(argument))
		parts = append(parts, argument[:n])
		argument = argument[n:]
	}
	return parts
}

func TestReadStreamAccumulatesLargeInterleavedToolArguments(t *testing.T) {
	alpha := string(rune(0x03b1))
	cjk := string(rune(0x754c))
	prefixA := `{"path":"` + alpha + `.txt","content":"`
	partsA := []string{prefixA, strings.Repeat("x", 2048-len(prefixA)), strings.Repeat("y", 2048), cjk + strings.Repeat(`\n`, 200) + `"}`}
	partsB := []string{`{"command":"`, strings.Repeat("echo ", 500), `done"}`}
	argumentA := strings.Join(partsA, "")
	argumentB := strings.Join(partsB, "")
	if !json.Valid([]byte(argumentA)) || !json.Valid([]byte(argumentB)) {
		t.Fatal("test fixture must contain valid JSON tool arguments")
	}

	parts := []anthropicToolArgumentPart{
		{index: 2, id: "tool_b", name: "bash", text: partsB[0], start: true},
		{index: 0, id: "tool_a", name: "write_file", text: partsA[0], start: true},
		{index: 2, text: partsB[1]},
		{index: 0, text: partsA[1]},
		{index: 2, text: partsB[2]},
		{index: 0, text: partsA[2]},
		{index: 0, text: partsA[3], stop: true},
		{index: 2, stop: true},
	}
	out := make(chan provider.Chunk, 32)
	c := &client{name: "test"}
	c.readStream(context.Background(), &http.Response{Body: io.NopCloser(strings.NewReader(anthropicToolArgumentStream(parts, true)))}, out)

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

	if len(starts) != 2 || starts[0].ID != "tool_b" || starts[1].ID != "tool_a" {
		t.Fatalf("tool starts = %+v, want calls b then a", starts)
	}
	if got, want := progress["tool_a"], []int{2048, 4096}; !slices.Equal(got, want) {
		t.Fatalf("tool_a progress = %v, want %v", got, want)
	}
	if got, want := progress["tool_b"], anthropicExpectedBucketProgress(partsB); !slices.Equal(got, want) {
		t.Fatalf("tool_b progress = %v, want %v", got, want)
	}
	if len(completed) != 2 {
		t.Fatalf("completed calls = %d, want 2", len(completed))
	}
	if completed[0].ID != "tool_a" || completed[0].Name != "write_file" || completed[0].Arguments != argumentA {
		t.Fatalf("first completed call = %+v", completed[0])
	}
	if completed[1].ID != "tool_b" || completed[1].Name != "bash" || completed[1].Arguments != argumentB {
		t.Fatalf("second completed call = %+v", completed[1])
	}
	if !done {
		t.Fatal("missing done chunk")
	}
}

func TestReadStreamDoesNotCompletePartialLargeToolArguments(t *testing.T) {
	parts := []anthropicToolArgumentPart{{index: 0, id: "tool_1", name: "write_file", text: strings.Repeat("x", 2048), start: true}}
	out := make(chan provider.Chunk, 8)
	c := &client{name: "test"}
	c.readStream(context.Background(), &http.Response{Body: io.NopCloser(strings.NewReader(anthropicToolArgumentStream(parts, false)))}, out)
	var started, progressed bool
	for chunk := range out {
		switch chunk.Type {
		case provider.ChunkToolCallStart:
			started = true
		case provider.ChunkToolCallArgsDelta:
			progressed = true
		case provider.ChunkToolCall:
			t.Fatalf("partial stream surfaced a complete tool call: %+v", chunk.ToolCall)
		}
	}
	if !started || !progressed {
		t.Fatalf("started=%v progressed=%v, want both liveness events", started, progressed)
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
			fragments := anthropicSplitArgument(argument, tc.fragment)
			parts := make([]anthropicToolArgumentPart, len(fragments))
			for i, fragment := range fragments {
				parts[i] = anthropicToolArgumentPart{index: 0, text: fragment}
			}
			parts[0].id, parts[0].name, parts[0].start = "tool_1", "write_file", true
			parts[len(parts)-1].stop = true
			fixture := anthropicToolArgumentStream(parts, true)
			c := &client{name: "benchmark"}
			var got string

			b.ReportAllocs()
			b.SetBytes(int64(len(argument)))
			for b.Loop() {
				out := make(chan provider.Chunk, len(argument)/2048+8)
				c.readStream(context.Background(), &http.Response{Body: io.NopCloser(strings.NewReader(fixture))}, out)
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
