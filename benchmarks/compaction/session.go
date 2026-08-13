package main

import (
	"fmt"
	"strings"

	"reasonix/internal/agent"
	"reasonix/internal/provider"
)

// A generation is one round of work followed by one compaction. Compaction
// re-derives its digest from the whole canonical transcript, so generation N
// folds everything generations 1..N produced — which is exactly the growth the
// cost arm measures and the drift arm probes.
const (
	unitsPerGeneration = 12
	toolResultBytes    = 6_000
)

// workUnit is one read_file round: the shape that dominates a real coding
// session's token weight.
func workUnit(sess *agent.Session, gen, i int) {
	id := fmt.Sprintf("g%02dc%02d", gen, i)
	path := fmt.Sprintf("internal/pkg%02d/file%02d.go", gen, i)
	sess.Add(provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{
		{ID: id, Name: "read_file", Arguments: fmt.Sprintf(`{"path":%q}`, path)},
	}})
	sess.Add(provider.Message{Role: provider.RoleTool, ToolCallID: id, Name: "read_file", Content: fakeGoFile(gen, i, toolResultBytes)})
	sess.Add(provider.Message{Role: provider.RoleAssistant, Content: fmt.Sprintf("Read %s; nothing surprising.", path)})
}

func fakeGoFile(gen, i, size int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "package pkg%02d\n\n// file%02d\n\n", gen, i)
	for line := 0; b.Len() < size; line++ {
		fmt.Fprintf(&b, "func helper%02d_%02d_%04d(x int) int { return x*%d + %d }\n", gen, i, line, line+3, line*7)
	}
	return b.String()
}

// growSession appends one generation of work, planting whatever probes belong
// to that generation so a fact's age is exactly the number of folds it has
// survived.
func growSession(sess *agent.Session, gen int, probes []probe) {
	for _, p := range probes {
		if p.plantAt == gen {
			p.plant(sess)
		}
		if revise, ok := p.later[gen]; ok {
			revise(sess)
		}
	}
	for i := range unitsPerGeneration {
		workUnit(sess, gen, i)
	}
}

func newSession() *agent.Session {
	sess := agent.NewSession("You are a terse coding agent. Answer questions from what you know; do not guess.")
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "Fix the config round-trip formatting bug in this repo."})
	return sess
}

// visibleContext splices the stored projection with the canonical messages
// appended after it — the same model-visible view the agent would send.
func visibleContext(sessionPath string, sess *agent.Session) ([]provider.Message, error) {
	st, ok, err := agent.LoadCompactionState(sessionPath)
	if err != nil {
		return nil, err
	}
	canonical := sess.Snapshot()
	if !ok || len(st.Projection.Messages) == 0 {
		return canonical, nil
	}
	out := append([]provider.Message(nil), st.Projection.Messages...)
	if n := st.Projection.CoveredCount; n >= 0 && n < len(canonical) {
		out = append(out, canonical[n:]...)
	}
	return out, nil
}
