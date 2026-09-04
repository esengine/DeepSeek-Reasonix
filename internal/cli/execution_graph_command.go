package cli

import (
	"fmt"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"

	"reasonix/internal/agentgraph"
	"reasonix/internal/control"
)

// showReport routes the two read-only reports. They share a branch because the
// switch they came from is already the largest function in the tree, and a
// report that only reads state has nothing to add to it.
func (m *chatTUI) showReport(cmd, input string) tea.Cmd {
	if cmd == "/graph" {
		return m.showExecutionGraph(input)
	}
	return m.showContextReport(input)
}

// showExecutionGraph prints the delegations this session opened and where each
// of them stands, rebuilt from what survived rather than from the event stream.
// It is the only way a headless run can ask what a fan-out did after the
// process that ran it is gone.
func (m *chatTUI) showExecutionGraph(input string) tea.Cmd {
	m.echoLocalCommand(input)
	m.commitLine(renderExecutionGraph(m.ctrl.ExecutionGraph()))
	return nil
}

// renderExecutionGraph lays the snapshot out one item per line: what it was,
// what it may touch, and what became of it. Interruptions are listed apart
// because they are not a state — they are executions nobody is running.
func renderExecutionGraph(snapshot control.ExecutionGraphSnapshot) string {
	workers := workerLines(snapshot.Graph)
	if len(workers) == 0 {
		return "This conversation has delegated nothing."
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d delegation(s):\n", len(workers))
	b.WriteString(strings.Join(workers, "\n"))
	if len(snapshot.Interruptions) > 0 {
		b.WriteString("\n\nno longer running, and not resumable:")
		for _, i := range snapshot.Interruptions {
			b.WriteString("\n  " + i.Execution + " — " + i.Kind)
		}
	}
	if len(snapshot.IdentityUnknown) > 0 {
		fmt.Fprintf(&b, "\n\n%d item(s) predate the worker record; their model and effort are unknown, not inherited.",
			len(snapshot.IdentityUnknown))
	}
	return b.String()
}

func workerLines(g agentgraph.Graph) []string {
	upstream := map[string][]string{}
	for _, e := range g.Edges {
		if e.Kind == agentgraph.Depends {
			upstream[e.To] = append(upstream[e.To], e.From)
		}
	}
	var out []string
	for _, n := range g.Nodes {
		if n.Kind != agentgraph.KindWorker {
			continue
		}
		line := "  " + n.ID + "  " + orUnknownState(n.State)
		if n.Grant != "" {
			line += " (" + string(n.Grant) + ")"
		}
		if n.Wait != "" {
			line += " first refused: " + string(n.Wait)
		}
		if up := upstream[n.ID]; len(up) > 0 {
			line += " after " + strings.Join(up, ", ")
		}
		if label := strings.TrimSpace(n.Label); label != "" {
			line += "  " + label
		}
		out = append(out, line)
	}
	sort.Strings(out)
	return out
}

// orUnknownState names the one state the vocabulary has no word for: an
// execution whose owner disappeared, which the rebuild refuses to call running.
func orUnknownState(state agentgraph.NodeState) string {
	if state == "" {
		return "interrupted"
	}
	return string(state)
}
