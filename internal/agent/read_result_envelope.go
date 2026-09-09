package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

// readResultEnvelopeFor builds the host-only envelope for one reader result:
// the reader supplies what it delivered and which store served it, and the host
// adds call identity, the logical read identity, and clips the envelope to the
// provider-visible bytes. ok=false means the tool cannot describe its delivery,
// so the message carries no envelope rather than a fabricated one.
func (a *Agent) readResultEnvelopeFor(ctx context.Context, call provider.ToolCall, o toolOutcome) (tool.ReadResultEnvelope, bool) {
	if a == nil || a.svc.tools == nil || o.errMsg != "" || o.blocked {
		return tool.ReadResultEnvelope{}, false
	}
	resolved, _, ambiguous := a.svc.tools.ResolveCall(call.Name)
	if resolved == nil || len(ambiguous) > 0 {
		return tool.ReadResultEnvelope{}, false
	}
	reader, ok := resolved.(tool.ReadEnvelopeProvider)
	if !ok {
		return tool.ReadResultEnvelope{}, false
	}
	raw := o.rawOutput
	if raw == "" {
		raw = o.output
	}
	env, ok := reader.ReadEnvelope(ctx, json.RawMessage(call.Arguments), raw)
	if o.readEnvelope != nil {
		env, ok = *o.readEnvelope, true
		if window, parsed := tool.ParseReadWindow(raw); (!parsed && len(env.DeliveredRanges) > 0) || (parsed && tool.WindowDigest(env.Source.CanonicalPath, window) != env.WindowDigest) {
			// An extension changed source text after execution. It remains a tool
			// result, but cannot inherit the reader's version or coverage proof.
			env.Source.Snapshot = ""
			env.DeliveredRanges, env.SourceEnd = nil, nil
			env.EOF, env.HasMore = false, true
		}
	}
	if !ok {
		return tool.ReadResultEnvelope{}, false
	}
	if o.rawOutput != "" && o.rawOutput != o.output {
		env = clipDeliveredRead(env, raw, o.output)
	}
	env.ResultRef = toolResultRef(call.ID, raw)
	env.ReadID = o.readTaskID
	if env.ReadID == "" {
		env.ReadID = incompleteReadID(call.ID, env.ResultRef, env.Source.CanonicalPath)
	}
	env.Source.WorkspaceID = a.workspaceID
	if cursor, ok := tool.DecodeReadCursor(env.NextCursor); ok {
		cursor.ReadID = env.ReadID
		if a.reads.tasks != nil {
			cursor.SessionID = a.reads.tasks.sessionID
			cursor.RunGen = a.reads.tasks.generation
			cursor.Binding = a.reads.tasks.binding
		}
		env.NextCursor = tool.EncodeReadCursor(cursor)
	}
	if !a.readPipelineActive() {
		a.reads.tasks.remember(env.ReadID, env, readPathArg(json.RawMessage(call.Arguments)))
	}
	return env, true
}

// Only complete, byte-identical numbered lines may become evidence. A cut in
// the middle of a numbered line is not a delivered source line.
func clipDeliveredRead(env tool.ReadResultEnvelope, raw, visible string) tool.ReadResultEnvelope {
	original, ok := tool.ParseReadWindow(raw)
	shown, shownOK := tool.ParseReadWindow(visible)
	if !ok || !shownOK {
		return env.ClipTo("")
	}
	var exact strings.Builder
	for i, line := range shown.Lines {
		number := shown.StartLine + i
		index := number - original.StartLine
		if index < 0 || index >= len(original.Lines) || line != original.Lines[index] {
			break
		}
		fmt.Fprintf(&exact, "%d→%s\n", number, line)
	}
	return env.ClipTo(exact.String())
}
