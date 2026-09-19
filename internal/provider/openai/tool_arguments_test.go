package openai

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

type toolArgumentPart struct {
	index          int
	id, name, text string
}

func openAIToolArgumentStream(parts []toolArgumentPart, complete bool) string {
	var stream strings.Builder
	for _, part := range parts {
		id, _ := json.Marshal(part.id)
		name, _ := json.Marshal(part.name)
		text, _ := json.Marshal(part.text)
		fmt.Fprintf(&stream, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":%d,\"id\":%s,\"function\":{\"name\":%s,\"arguments\":%s}}]}}]}\n\n", part.index, id, name, text)
	}
	if complete {
		stream.WriteString("data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\n")
		stream.WriteString("data: [DONE]\n\n")
	}
	return stream.String()
}

func expectedBucketProgress(parts []string) []int {
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

func splitArgument(argument string, size int) []string {
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

	parts := []toolArgumentPart{
		{index: 1, id: "call_b", name: "bash", text: partsB[0]},
		{index: 0, id: "call_a", name: "write_file", text: partsA[0]},
		{index: 1, text: partsB[1]},
		{index: 0, text: partsA[1]},
		{index: 1, text: partsB[2]},
		{index: 0, text: partsA[2]},
		{index: 0, text: partsA[3]},
	}
	out := make(chan provider.Chunk, 32)
	c := &client{name: "test"}
	emitted, err := c.readStream(context.Background(), &http.Response{Body: io.NopCloser(strings.NewReader(openAIToolArgumentStream(parts, true)))}, out)
	if err != nil {
		t.Fatalf("readStream: %v", err)
	}
	if !emitted {
		t.Fatal("readStream reported no emitted output")
	}
	close(out)

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

	if len(starts) != 2 || starts[0].ID != "call_b" || starts[1].ID != "call_a" {
		t.Fatalf("tool starts = %+v, want calls b then a", starts)
	}
	if got, want := progress["call_a"], []int{2048, 4096}; !slices.Equal(got, want) {
		t.Fatalf("call_a progress = %v, want %v", got, want)
	}
	if got, want := progress["call_b"], expectedBucketProgress(partsB); !slices.Equal(got, want) {
		t.Fatalf("call_b progress = %v, want %v", got, want)
	}
	if len(completed) != 2 {
		t.Fatalf("completed calls = %d, want 2", len(completed))
	}
	if completed[0].ID != "call_a" || completed[0].Name != "write_file" || completed[0].Arguments != argumentA {
		t.Fatalf("first completed call = %+v", completed[0])
	}
	if completed[1].ID != "call_b" || completed[1].Name != "bash" || completed[1].Arguments != argumentB {
		t.Fatalf("second completed call = %+v", completed[1])
	}
	if !done {
		t.Fatal("missing done chunk")
	}
}

func TestReadStreamDoesNotCompletePartialLargeToolArguments(t *testing.T) {
	partial := strings.Repeat("x", 2048)
	parts := []toolArgumentPart{{index: 0, id: "call_1", name: "write_file", text: partial}}
	out := make(chan provider.Chunk, 8)
	c := &client{name: "test"}
	emitted, err := c.readStream(context.Background(), &http.Response{Body: io.NopCloser(strings.NewReader(openAIToolArgumentStream(parts, false)))}, out)
	if !emitted || !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("readStream emitted=%v err=%v, want emitted output and unexpected EOF", emitted, err)
	}
	close(out)
	var started, progressed bool
	for chunk := range out {
		switch chunk.Type {
		case provider.ChunkToolCallStart:
			started = true
		case provider.ChunkToolCallArgsDelta:
			progressed = true
		case provider.ChunkToolCall, provider.ChunkDone:
			t.Fatalf("partial stream surfaced terminal chunk: %+v", chunk)
		}
	}
	if !started || !progressed {
		t.Fatalf("started=%v progressed=%v, want both liveness events", started, progressed)
	}
}

func BenchmarkReadStreamToolArguments(b *testing.B) {
	for _, tc := range []struct {
		name            string
		total, fragment int
	}{
		{name: "64B/one-fragment", total: 64, fragment: 64},
		{name: "512B/128B-fragments", total: 512, fragment: 128},
		{name: "2KiB/128B-fragments", total: 2 << 10, fragment: 128},
		{name: "4KiB/128B-fragments", total: 4 << 10, fragment: 128},
		{name: "32KiB/128B-fragments", total: 32 << 10, fragment: 128},
		{name: "128KiB/128B-fragments", total: 128 << 10, fragment: 128},
	} {
		b.Run(tc.name, func(b *testing.B) {
			prefix, suffix := `{"content":"`, `"}`
			argument := prefix + strings.Repeat("x", tc.total-len(prefix)-len(suffix)) + suffix
			fragments := splitArgument(argument, tc.fragment)
			parts := make([]toolArgumentPart, len(fragments))
			for i, fragment := range fragments {
				parts[i] = toolArgumentPart{index: 0, text: fragment}
			}
			parts[0].id, parts[0].name = "call_1", "write_file"
			fixture := openAIToolArgumentStream(parts, true)
			c := &client{name: "benchmark"}
			var got string

			b.ReportAllocs()
			b.SetBytes(int64(len(argument)))
			for b.Loop() {
				out := make(chan provider.Chunk, len(argument)/2048+8)
				_, err := c.readStream(context.Background(), &http.Response{Body: io.NopCloser(strings.NewReader(fixture))}, out)
				if err != nil {
					b.Fatal(err)
				}
				close(out)
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
