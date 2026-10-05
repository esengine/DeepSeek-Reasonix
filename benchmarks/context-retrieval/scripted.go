package main

import (
	"context"
	"encoding/json"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"reasonix/internal/contract/provider"
)

// scriptedProvider walks the chain a competent model would: search, read the
// best hit, answer with what came back. It proves the pipeline holds together
// for nothing, so a real run's first failure is about the model.

type scriptedProvider struct{}

func (p *scriptedProvider) Name() string { return "contextbench-scripted" }

var (
	scriptedHit    = regexp.MustCompile(`(?m)^#(\d+) `)
	scriptedRecall = regexp.MustCompile(`(?s)^#\d+\n(.*)`)
)

// Stream decides from the request alone, never from a call counter: one
// provider serves every task in a run, and a counter would make the second task
// start where the first left off.
func (p *scriptedProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	ch := make(chan provider.Chunk, 3)
	defer close(ch)
	// Honour the schema, as a model does: an arm without a query parameter is
	// an arm that cannot search, and pretending otherwise scores a refusal as
	// a failed search.
	if !recallAccepts(req, "query") {
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "I have no address for that part of the history."}
		ch <- provider.Chunk{Type: provider.ChunkDone}
		return ch, nil
	}
	// The last message, not the last tool result anywhere: the fixture's own
	// history is full of tool results, and one of those is not this turn's.
	last := ""
	if n := len(req.Messages); n > 0 && req.Messages[n-1].Role == provider.RoleTool {
		last = req.Messages[n-1].Content
	}
	switch {
	case last == "":
		args, _ := json.Marshal(map[string]string{"query": lastUserText(req)})
		ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{
			ID: "s1", Name: "recall", Arguments: string(args),
		}}
	case strings.Contains(last, "Folded context matching"):
		positions := scriptedHit.FindAllStringSubmatch(last, 1)
		if len(positions) == 0 {
			ch <- provider.Chunk{Type: provider.ChunkText, Text: "Nothing in the folded history matches."}
			break
		}
		n, _ := strconv.Atoi(positions[0][1])
		args, _ := json.Marshal(map[string][]int{"positions": {n}})
		ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{
			ID: "r1", Name: "recall", Arguments: string(args),
		}}
	default:
		// Answer with what recall returned, so a correct chain scores as one.
		body := last
		if m := scriptedRecall.FindStringSubmatch(last); len(m) > 1 {
			body = m[1]
		}
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "From the folded history: " + body}
	}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	return ch, nil
}

// recallAccepts reports whether recall is offered with the given parameter.
func recallAccepts(req provider.Request, param string) bool {
	for _, t := range req.Tools {
		if t.Name != "recall" {
			continue
		}
		b, err := json.Marshal(t.Parameters)
		return err == nil && strings.Contains(string(b), `"`+param+`"`)
	}
	return false
}

func lastUserText(req provider.Request) string {
	for _, m := range slices.Backward(req.Messages) {
		if m.Role == provider.RoleUser {
			return m.Content
		}
	}
	return ""
}
