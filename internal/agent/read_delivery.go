package agent

import (
	"context"
	"crypto/sha256"
	"fmt"
	"sort"

	"reasonix/internal/provider"
	"reasonix/internal/readcoord"
	"reasonix/internal/tool"
)

// readDelivery contains no source text. References always name an original
// delivery, never another reference. Both maps are owned by the run loop.
type readDelivery struct {
	callID    string
	resultRef string
	source    tool.ReadResultSource
	digest    [32]byte
	ranges    []tool.ReadRange
}

func (a *Agent) rememberReadDelivery(callID, body string, env tool.ReadResultEnvelope) {
	if !a.readPipelineActive() || len(env.DeliveredRanges) == 0 || env.Source.Identity == "" || env.Source.Snapshot == "" {
		return
	}
	if a.reads.deliveries == nil {
		a.reads.deliveries = make(map[string]readDelivery)
	}
	a.reads.deliveries[callID] = readDelivery{callID: callID, resultRef: env.ResultRef, source: env.Source, digest: sha256.Sum256([]byte(body)), ranges: append([]tool.ReadRange(nil), env.DeliveredRanges...)}
}

// freezeVisibleReads is called for the exact request used by a sampling
// attempt, after projection, interception and admission. Missing or rewritten
// original text makes a reference ineligible, even if RawContent is retained.
func (a *Agent) freezeVisibleReads(messages []provider.Message) {
	visible := make(map[string]readDelivery)
	for _, msg := range messages {
		if msg.Role != provider.RoleTool || msg.LocalOnly {
			continue
		}
		if d, ok := a.reads.deliveries[msg.ToolCallID]; ok && sha256.Sum256([]byte(msg.Content)) == d.digest {
			visible[msg.ToolCallID] = d
		}
	}
	a.reads.visible = visible
}

func (a *Agent) finalizedReadEnvelope(ctx context.Context, call provider.ToolCall, o toolOutcome) (tool.ReadResultEnvelope, bool) {
	if o.finalReadEnvelope != nil {
		return *o.finalReadEnvelope, true
	}
	return a.readResultEnvelopeFor(ctx, call, o)
}

func (a *Agent) finalizeReadDelivery(ctx context.Context, call provider.ToolCall, o *toolOutcome) {
	if !a.readPipelineActive() {
		return
	}
	env, ok := a.readResultEnvelopeFor(ctx, call, *o)
	if !ok {
		return
	}
	env.ReadID = a.turn.readShadow.coord.Associate(env)
	o.readTaskID = env.ReadID
	// A reference cannot create coverage. Only already covered windows can
	// be omitted, and only when their original text was in this model request.
	ob, known := a.turn.readShadow.coord.Get(env.ReadID)
	if known && ob.Version == env.Source.Snapshot && len(env.DeliveredRanges) > 0 && readcoord.Covers(ob.Covered, env.DeliveredRanges) && a.readReferenceHasCurrentEvidence(env, o.output) {
		ids := make([]string, 0, len(a.reads.visible))
		for id := range a.reads.visible {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		digest := sha256.Sum256([]byte(o.output))
		for _, id := range ids {
			d := a.reads.visible[id]
			if d.source != env.Source || d.digest != digest || !readcoord.Covers(d.ranges, env.DeliveredRanges) {
				continue
			}
			if o.rawOutput == "" {
				o.rawOutput = o.output
			}
			status := "This window was already delivered; the outstanding requirement still needs its missing ranges."
			if ob.State == readcoord.StateSatisfied {
				status = "The requested read requirement is already satisfied on this unchanged source version."
			}
			o.output = fmt.Sprintf("Repeated read: original text is available in call_id=%s. %s", d.callID, status)
			o.readReference = &d
			env = env.ClipTo("")
			break
		}
	}
	// Rebind any source cursor after association, then remember the final ID.
	if cursor, valid := tool.DecodeReadCursor(env.NextCursor); valid {
		cursor.ReadID = env.ReadID
		env.NextCursor = tool.EncodeReadCursor(cursor)
	}
	a.reads.tasks.remember(env.ReadID, env, readPathArg([]byte(call.Arguments)))
	o.finalReadEnvelope = &env
}

// Read coverage survives writes, but edit evidence must follow the last write
// and reach a provider boundary. Re-deliver when deduplication would otherwise
// suppress the fresh observation needed to repair a rejected edit.
func (a *Agent) readReferenceHasCurrentEvidence(env tool.ReadResultEnvelope, output string) bool {
	if a.task.ledger == nil {
		return true
	}
	if _, written := a.task.ledger.LatestSuccessfulWriteIndex([]string{env.Source.CanonicalPath}); !written {
		return true
	}
	w, ok := tool.ParseReadWindow(output)
	if !ok {
		return false
	}
	target := tool.EvidenceTargetInfo{Path: env.Source.CanonicalPath, Snapshot: env.Source.Snapshot, Ranges: env.DeliveredRanges}
	for _, line := range w.Lines {
		target.Hashes = append(target.Hashes, hashLine(line))
	}
	observations := a.eligibleObservations(target.Path, a.task.ledger.ObservationBoundary())
	satisfied, _ := evidenceCoversTarget(observations, target)
	return satisfied
}
