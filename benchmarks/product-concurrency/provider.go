package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"reasonix/internal/contract/provider"
)

type simulatedProvider struct {
	request     request
	clock       func() int64
	result      func(string) (string, bool, bool)
	phases      []interval
	step        int
	read        bool
	sequence    int
	pending     *provider.ToolCall
	conflicts   int
	reschedules int
	verified    bool
	admitted    bool
}

func (*simulatedProvider) Name() string { return "product-concurrency" }

func (p *simulatedProvider) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	started := p.clock()
	timer := time.NewTimer(p.request.Load.Think)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-timer.C:
	}
	p.phases = append(p.phases, interval{"provider_wait", started, p.clock()})
	if p.pending != nil {
		code, success, found := p.result(p.pending.ID)
		if found {
			if !success {
				if code != "workspace.mutation_version_conflict" {
					return nil, fmt.Errorf("unexpected tool failure: %s", code)
				}
				p.conflicts++
				p.reschedules++
				p.read = false
				if p.reschedules > 128 {
					return nil, fmt.Errorf("fixture retry bound exceeded")
				}
			} else if p.pending.ID == "admission" {
				p.admitted = true
			} else if p.pending.ID == "verify" {
				p.verified = true
			} else if p.pending.Name == "read_file" {
				p.read = true
			} else {
				p.step++
				p.read = false
			}
		}
		if !found {
			return nil, fmt.Errorf("missing real tool result %s", p.pending.ID)
		}
	}
	ch := make(chan provider.Chunk, 2)
	if !p.admitted {
		raw, _ := json.Marshal(map[string]string{"command": fmt.Sprintf("printf admitted > .worker-%d-admission", p.request.ID)})
		p.pending = &provider.ToolCall{ID: "admission", Name: "bash", Arguments: string(raw)}
		ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: p.pending}
	} else if p.step == p.request.Load.Steps && p.verified {
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "fixture changes completed"}
	} else if p.step == p.request.Load.Steps {
		raw, _ := json.Marshal(map[string]string{"command": fmt.Sprintf("BENCH_WORKER=%d BENCH_STEPS=%d BENCH_SCOPE=%s go test -count=1 ./...", p.request.ID, p.request.Load.Steps, p.request.Scope)})
		p.pending = &provider.ToolCall{ID: "verify", Name: "bash", Arguments: string(raw)}
		ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: p.pending}
	} else {
		name, args := p.next()
		raw, err := json.Marshal(args)
		if err != nil {
			return nil, err
		}
		p.sequence++
		p.pending = &provider.ToolCall{ID: fmt.Sprintf("worker-%d-call-%d", p.request.ID, p.sequence), Name: name, Arguments: string(raw)}
		ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: p.pending}
	}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func (p *simulatedProvider) next() (string, any) {
	id, step := p.request.ID, p.step
	switch p.request.Scope {
	case "same-file":
		if !p.read {
			return "read_file", map[string]string{"path": "shared.txt"}
		}
		return "edit_file", map[string]string{"path": "shared.txt", "old_string": fmt.Sprintf("worker-%d=%d\n", id, step), "new_string": fmt.Sprintf("worker-%d=%d\n", id, step+1)}
	case "opaque":
		return "bash", map[string]string{"command": fmt.Sprintf("printf 'step-%d\\n' >> worker-%d.txt", step, id)}
	default:
		return "write_file", map[string]string{"path": fmt.Sprintf("worker-%d-step-%d.txt", id, step), "content": fmt.Sprintf("worker-%d-step-%d\n", id, step)}
	}
}
