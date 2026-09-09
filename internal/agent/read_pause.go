package agent

import (
	"errors"

	"reasonix/internal/provider"
	"reasonix/internal/readcoord"
	"reasonix/internal/tool"
)

// finishReadRun records the verdict before closeReadStatuses cancels active
// tasks. The LocalOnly sentinel keeps older provider projections safe too.
func (a *Agent) finishReadRun(err error) {
	defer a.closeReadStatuses()
	defer func() { a.reads.deliveries, a.reads.visible = nil, nil }()
	var incomplete *IncompleteReadError
	if !errors.As(err, &incomplete) || !a.readPipelineActive() {
		return
	}
	pause := &provider.ReadPause{ID: a.reads.tasks.binding, Reads: []provider.PausedRead{}}
	for _, ob := range a.turn.readShadow.coord.Snapshot() {
		if ob.State.Terminal() {
			continue
		}
		if len(pause.Reads) == 32 {
			pause.Omitted++
			continue
		}
		reason := "incomplete"
		if ob.Stop != nil {
			reason = ob.Stop.Code
		}
		var missing []tool.ReadRange
		if ob.Requirement.WholeFile && ob.SourceEnd != nil {
			missing = []tool.ReadRange{{Start: 0, End: *ob.SourceEnd}}
		} else {
			missing = ob.Requirement.Ranges
		}
		pause.Reads = append(pause.Reads, provider.PausedRead{ReadID: ob.Key, Path: ob.Scope.CanonicalPath, Intent: string(ob.Requirement.Intent), Covered: boundedReadRanges(ob.Covered), Missing: boundedReadRanges(readcoord.Subtract(missing, ob.Covered)), Reason: reason})
	}
	incomplete.Pause = pause
	a.sess.conversation.Add(provider.Message{Role: provider.RoleTool, ToolCallID: provider.LocalOnlyToolID, Name: provider.LocalOnlyToolName, LocalOnly: true, ReadPause: pause})
}

func boundedReadRanges(ranges []tool.ReadRange) [][2]int {
	result := make([][2]int, 0, min(64, len(ranges)))
	for _, r := range ranges[:min(64, len(ranges))] {
		result = append(result, [2]int{r.Start, r.End})
	}
	return result
}
