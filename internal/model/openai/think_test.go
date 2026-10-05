package openai

import "testing"

func runSplitter(deltas []string) (reasoning, text string) {
	var t thinkSplitter
	for _, d := range deltas {
		r, txt := t.push(d)
		reasoning += r
		text += txt
	}
	r, txt := t.flush()
	return reasoning + r, text + txt
}

func TestThinkSplitter(t *testing.T) {
	cases := []struct {
		name      string
		deltas    []string
		reasoning string
		text      string
	}{
		{
			name:      "whole block in one delta",
			deltas:    []string{"<think>reasoning here</think>the answer"},
			reasoning: "reasoning here",
			text:      "the answer",
		},
		{
			name:      "open tag split across deltas",
			deltas:    []string{"<th", "ink>chain", " of thought</think>answer"},
			reasoning: "chain of thought",
			text:      "answer",
		},
		{
			name:      "close tag split across deltas",
			deltas:    []string{"<think>thinking</thi", "nk>done"},
			reasoning: "thinking",
			text:      "done",
		},
		{
			name:      "leading whitespace before think is dropped",
			deltas:    []string{"\n\n  <think>r</think>\n\nanswer"},
			reasoning: "r",
			text:      "answer",
		},
		{
			name:      "no think tag passes through as text",
			deltas:    []string{"just a normal ", "answer"},
			reasoning: "",
			text:      "just a normal answer",
		},
		{
			name:      "think mentioned mid-answer is not hijacked",
			deltas:    []string{"the model emits <think> tags around its reasoning"},
			reasoning: "",
			text:      "the model emits <think> tags around its reasoning",
		},
		{
			name:      "unterminated think block flushes as reasoning",
			deltas:    []string{"<think>still thinking when the stream ended"},
			reasoning: "still thinking when the stream ended",
			text:      "",
		},
		{
			name:      "per-character streaming",
			deltas:    []string{"<", "t", "h", "i", "n", "k", ">", "a", "</", "think>", "b"},
			reasoning: "a",
			text:      "b",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, txt := runSplitter(tc.deltas)
			if r != tc.reasoning {
				t.Errorf("reasoning = %q, want %q", r, tc.reasoning)
			}
			if txt != tc.text {
				t.Errorf("text = %q, want %q", txt, tc.text)
			}
		})
	}
}
