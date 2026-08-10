package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"reasonix/internal/ablation"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

const (
	maxCompressAnchorBytes = 512
	maxCompressFocusBytes  = 2000
)

var errCompressStaleContext = errors.New("compress: conversation changed while compression was running; retry with the current context")

// CompressContext implements the context-bound compress tool. It resolves the
// anchor against the current model-visible view and installs a projection only;
// the canonical transcript and checkpoint lineage remain untouched.
func (a *Agent) CompressContext(ctx context.Context, req tool.CompressRequest) (tool.CompressResult, error) {
	direction := strings.TrimSpace(req.Direction)
	anchor := strings.TrimSpace(req.Anchor)
	focus := strings.TrimSpace(req.Focus)
	if direction != "before" && direction != "after" {
		return tool.CompressResult{}, fmt.Errorf("compress: direction must be before or after")
	}
	if anchor == "" {
		return tool.CompressResult{}, fmt.Errorf("compress: anchor must not be empty")
	}
	if len(anchor) > maxCompressAnchorBytes {
		return tool.CompressResult{}, fmt.Errorf("compress: anchor exceeds %d bytes", maxCompressAnchorBytes)
	}
	if len(focus) > maxCompressFocusBytes {
		return tool.CompressResult{}, fmt.Errorf("compress: focus exceeds %d bytes", maxCompressFocusBytes)
	}

	snap := a.snapshotExplicitCompression()
	matches := make([]int, 0, 2)
	for i, msg := range snap.visible {
		if !compressAnchorCandidate(msg) {
			continue
		}
		if strings.Contains(UserMessageText(msg), anchor) {
			matches = append(matches, i)
		}
	}
	if len(matches) == 0 {
		return tool.CompressResult{}, fmt.Errorf("compress: anchor did not match any current user message; retry with an exact excerpt from a visible user turn")
	}
	if len(matches) > 1 {
		return tool.CompressResult{}, fmt.Errorf("compress: anchor matched %d user messages; retry with a longer unique excerpt", len(matches))
	}

	return a.compressVisibleRange(ctx, snap, CompactionTriggerTool, direction, matches[0], anchorPreview(UserMessageText(snap.visible[matches[0]])), focus)
}

type explicitCompressionSnapshot struct {
	canonical         []provider.Message
	visible           []provider.Message
	transcriptVersion uint64
	coveredHash       string
	projectionVersion uint64
	generation        uint64
	promptCacheKey    string
}

func (a *Agent) snapshotExplicitCompression() explicitCompressionSnapshot {
	canonical, version := a.session.snapshotMessagesVersion()
	cacheKey := a.currentPromptCacheKey()
	a.compactionMu.Lock()
	state := a.compactionState
	a.compactionMu.Unlock()
	visible := canonical
	if projectionValid(state, canonical, version, cacheKey) {
		if projected := modelVisibleFromProjection(state.Projection, canonical); len(projected) > 0 {
			visible = projected
		}
	}
	return explicitCompressionSnapshot{
		canonical:         canonical,
		visible:           compressionVisibleMessages(visible),
		transcriptVersion: version,
		coveredHash:       coveredPrefixHash(canonical, len(canonical)),
		projectionVersion: state.Projection.ProjectionVersion,
		generation:        state.Generation,
		promptCacheKey:    cacheKey,
	}
}

func compressionVisibleMessages(msgs []provider.Message) []provider.Message {
	out := make([]provider.Message, 0, len(msgs)+1)
	for _, msg := range msgs {
		if !msg.LocalOnly {
			summary, user, split := splitLegacyCoalescedSummary(msg)
			if split {
				out = append(out, summary, user)
			} else {
				out = append(out, msg)
			}
		}
	}
	return out
}

// Older schema-v1 sidecars may have persisted a strict-role merge of the
// summary and its following user turn. Split that legacy shape for range
// planning; new sidecars keep the logical messages separate and coalesce only
// on the provider request copy.
func splitLegacyCoalescedSummary(msg provider.Message) (provider.Message, provider.Message, bool) {
	if !isCompactionSummary(msg) {
		return provider.Message{}, provider.Message{}, false
	}
	separator := summaryTagClose + "\n\n"
	i := strings.Index(msg.Content, separator)
	if i < 0 || i+len(separator) >= len(msg.Content) {
		return provider.Message{}, provider.Message{}, false
	}
	summary := msg
	summary.Content = msg.Content[:i+len(summaryTagClose)]
	summary.RawContent = ""
	summary.Images = nil
	summary.ToolCalls = nil
	summary.ResponsesItems = nil
	summary.CreatedAt = 0
	user := msg
	user.Content = msg.Content[i+len(separator):]
	user.RawContent = ""
	return summary, user, true
}

func compressAnchorCandidate(msg provider.Message) bool {
	if msg.Role != provider.RoleUser || msg.LocalOnly || isCompactionSummary(msg) {
		return false
	}
	return IsUserAuthoredTurn(UserMessageText(msg))
}

func anchorPreview(text string) string {
	return truncatePreview(previewProse(text))
}

type visibleCompressionPlan struct {
	result    tool.CompressResult
	foldMask  []bool
	fold      []provider.Message
	firstFold int
}

type preparedVisibleCompression struct {
	fold         []provider.Message
	instructions string
}

func (a *Agent) compressVisibleRange(
	ctx context.Context,
	snap explicitCompressionSnapshot,
	trigger string,
	direction string,
	anchorIndex int,
	preview string,
	instructions string,
) (tool.CompressResult, error) {
	a.compactionRunMu.Lock()
	defer a.compactionRunMu.Unlock()
	if !a.explicitCompressionSnapshotCurrent(snap) {
		return tool.CompressResult{}, errCompressStaleContext
	}
	plan, ok := a.planVisibleCompression(snap, direction, anchorIndex, preview)
	if !ok {
		return plan.result, nil
	}
	result := plan.result

	a.sink.Emit(event.Event{Kind: event.CompactionStarted, Compaction: event.Compaction{Trigger: trigger}})
	prepared, reason, err := a.prepareVisibleCompression(ctx, trigger, plan.fold, instructions)
	if err != nil {
		a.emitCompactionAborted(trigger)
		return tool.CompressResult{}, err
	}
	if reason != "" {
		a.emitCompactionAborted(trigger)
		result.Reason = reason
		return result, nil
	}

	res, err := a.foldToSummary(ctx, prepared.fold, prepared.instructions)
	summary := res.Text
	tele := compactionTelemetryFromSummary(trigger, a.CacheState(), result.SourceTokens, res)
	if err != nil {
		tele.Error = err.Error()
		a.emitCompactionTelemetry(tele)
		a.emitCompactionAborted(trigger)
		return tool.CompressResult{}, err
	}
	summary, err = a.interceptCompactionComplete(ctx, summary)
	if err != nil {
		tele.Error = err.Error()
		a.emitCompactionTelemetry(tele)
		a.emitCompactionAborted(trigger)
		return tool.CompressResult{}, err
	}

	projection := buildVisibleCompressionProjection(snap.visible, plan, summary)
	projectionTokens := estimateMessagesTokens(a.providerProjectionMessages(projection))
	tele.ProjectionTokens = projectionTokens
	result.Messages = len(plan.fold)
	result.ProjectionTokens = projectionTokens
	result.Mode = res.Mode
	if projectionTokens >= result.SourceTokens {
		result.Reason = "compressed context would not be smaller"
		a.emitCompactionTelemetry(tele)
		a.emitCompactionAborted(trigger)
		return result, nil
	}

	inputHash := providerVisibleFingerprint(provider.ModelMessages(snap.visible))
	outputHash := providerVisibleFingerprint(projection)
	state, err := a.commitSummaryProjection(summaryProjectionCommit{
		canonical: snap.canonical, fold: prepared.fold, projected: projection, result: res,
		transcriptVersion: snap.transcriptVersion, projectionVersion: snap.projectionVersion, generation: snap.generation,
		activeTurn: a.activeTurnCreatedAt.Load(), trigger: trigger, summary: summary,
		inputHash: inputHash, outputHash: outputHash, sourceTokens: result.SourceTokens, projectionTokens: projectionTokens,
	})
	if err != nil {
		if errors.Is(err, errCompressStaleContext) {
			tele.Error = err.Error()
			a.emitCompactionTelemetry(tele)
		}
		a.emitCompactionAborted(trigger)
		return tool.CompressResult{}, err
	}
	a.emitCompactionTelemetry(tele)
	a.sink.Emit(event.Event{Kind: event.CompactionDone, Compaction: event.Compaction{
		Trigger: trigger, Messages: len(plan.fold), Summary: summary, Archive: state.LastReceipt.Archive,
	}})
	result.Status = "ok"
	result.Reason = ""
	return result, nil
}

func (a *Agent) explicitCompressionSnapshotCurrent(snap explicitCompressionSnapshot) bool {
	current, version := a.session.snapshotMessagesVersion()
	a.compactionMu.Lock()
	projectionVersion := a.compactionState.Projection.ProjectionVersion
	generation := a.compactionState.Generation
	a.compactionMu.Unlock()
	return version == snap.transcriptVersion && len(current) == len(snap.canonical) &&
		coveredPrefixHash(current, len(current)) == snap.coveredHash &&
		projectionVersion == snap.projectionVersion && generation == snap.generation &&
		a.currentPromptCacheKey() == snap.promptCacheKey
}

func (a *Agent) planVisibleCompression(snap explicitCompressionSnapshot, direction string, anchorIndex int, preview string) (visibleCompressionPlan, bool) {
	sourceTokens := estimateMessagesTokens(snap.visible)
	plan := visibleCompressionPlan{result: tool.CompressResult{
		Status:           "noop",
		Direction:        direction,
		Anchor:           preview,
		SourceTokens:     sourceTokens,
		ProjectionTokens: sourceTokens,
	}}
	if anchorIndex < 0 || anchorIndex >= len(snap.visible) {
		plan.result.Reason = "anchor is no longer present in the model context"
		return plan, false
	}
	head := 0
	if len(snap.visible) > 0 && snap.visible[0].Role == provider.RoleSystem {
		head = 1
	}
	completedEnd := len(snap.visible)
	if active := a.activeTurnStart(snap.visible); active >= 0 {
		completedEnd = active
	}
	start, end := head, anchorIndex
	if direction == "after" {
		start, end = anchorIndex, completedEnd
	}
	if start < head {
		start = head
	}
	if end > completedEnd {
		end = completedEnd
	}
	if start >= end {
		plan.result.Reason = "selected range is empty"
		return plan, false
	}

	plan.foldMask = make([]bool, len(snap.visible))
	plan.firstFold = len(snap.visible)
	for i, msg := range snap.visible {
		selected := i >= start && i < end
		mergeSummary := i < completedEnd && isCompactionSummary(msg)
		if msg.Role == provider.RoleSystem || i < head || (!selected && !mergeSummary) {
			continue
		}
		plan.foldMask[i] = true
		plan.fold = append(plan.fold, msg)
		if i < plan.firstFold {
			plan.firstFold = i
		}
	}
	if len(plan.fold) == 0 {
		plan.result.Reason = "selected range has no model-visible messages"
		return plan, false
	}
	return plan, true
}

func (a *Agent) prepareVisibleCompression(ctx context.Context, trigger string, fold []provider.Message, instructions string) (preparedVisibleCompression, string, error) {
	if a.hooks != nil {
		if hookInstructions := a.hooks.PreCompact(ctx, trigger); hookInstructions != "" {
			if instructions != "" {
				instructions += "\n"
			}
			instructions += hookInstructions
		}
	}
	preparedFold, preparedInstructions, err := a.interceptCompactionPrepare(ctx, fold, instructions)
	if err != nil {
		return preparedVisibleCompression{}, "", err
	}
	preparedFold = provider.ModelMessages(preparedFold)
	if len(preparedFold) == 0 {
		return preparedVisibleCompression{}, "compaction hook removed the selected range", nil
	}
	return preparedVisibleCompression{fold: preparedFold, instructions: preparedInstructions}, "", nil
}

func buildVisibleCompressionProjection(visible []provider.Message, plan visibleCompressionPlan, summary string) []provider.Message {
	projection := make([]provider.Message, 0, len(visible)-len(plan.fold)+1)
	for i, msg := range visible {
		if i == plan.firstFold {
			projection = append(projection, formatSummaryMessage(summary))
		}
		if !plan.foldMask[i] {
			projection = append(projection, msg)
		}
	}
	return provider.ModelMessages(projection)
}

func compactionTelemetryFromSummary(trigger, cacheState string, sourceTokens int, res foldSummary) CompactionTelemetry {
	tele := CompactionTelemetry{
		Trigger: trigger, CacheState: cacheState, Mode: res.Mode,
		Native: res.Mode == CompactionModeNative, SourceTokens: sourceTokens,
		ProviderRequestID: res.RequestID,
		FoldTokens:        res.FoldTokens,
		Spans:             res.Spans,
	}
	usage := res.Usage
	if usage == nil {
		return tele
	}
	tele.InputTokens = usage.PromptTokens
	tele.OutputTokens = usage.CompletionTokens
	tele.CacheHitTokens = usage.CacheHitTokens
	tele.CacheMissTokens = usage.CacheMissTokens
	tele.CacheWriteTokens = usage.CacheWriteTokens
	tele.RequestCount = usage.RequestCount
	if tele.RequestCount <= 0 {
		tele.RequestCount = 1
	}
	return tele
}

// compact writes a context projection; trigger stays "auto"/"manual" for UI cards.
func (a *Agent) compact(ctx context.Context, trigger, instructions string, force bool) error {
	_, err := a.compactToProjection(ctx, trigger, instructions, force)
	return err
}

// compactToProjection summarizes the older middle of the session into a model-
// visible projection. The canonical transcript is never rewritten. force
// bypasses the fold-economics skip. CompactionNoop means no projection was
// installed (nothing to fold); callers at the force threshold must treat that
// as a hard failure rather than sending the oversized canonical prompt.
func (a *Agent) compactToProjection(ctx context.Context, trigger, instructions string, force bool) (CompactionOutcome, error) {
	a.compactionRunMu.Lock()
	defer a.compactionRunMu.Unlock()
	activeTurn := a.activeTurnCreatedAt.Load()
	if activeTurn != 0 && a.lastCompactionTurn.Load() == activeTurn && trigger != CompactionTriggerManual {
		return CompactionNoop, nil
	}
	canonical, transcriptVersion := a.session.snapshotMessagesVersion()
	a.compactionMu.Lock()
	stateSnapshot := a.compactionState
	startProjectionVersion := a.compactionState.Projection.ProjectionVersion
	startGeneration := a.compactionState.Generation
	a.compactionMu.Unlock()
	visibleInput := canonical
	if projectionValid(stateSnapshot, canonical, transcriptVersion, a.currentPromptCacheKey()) {
		if projected := modelVisibleFromProjection(stateSnapshot.Projection, canonical); len(projected) > 0 {
			visibleInput = projected
		}
	}
	viewInputHash := providerVisibleFingerprint(provider.ModelMessages(visibleInput))
	if trigger != CompactionTriggerManual && stateSnapshot.LastReceipt != nil && stateSnapshot.LastReceipt.Status == "applied" && stateSnapshot.LastReceipt.Action == "summary" && stateSnapshot.LastReceipt.InputHash == viewInputHash {
		return CompactionNoop, nil
	}
	msgs := a.foldSource(canonical)
	head, start, ok := a.planFoldRegion(msgs)
	if !ok {
		return CompactionNoop, nil
	}
	region := msgs[head:start]
	early, carried, kept, fold := a.partitionFoldForProjection(region)
	if len(fold) == 0 {
		return CompactionNoop, nil
	}
	if !force && !foldEconomics(fold) {
		return CompactionNoop, nil
	}

	a.sink.Emit(event.Event{Kind: event.CompactionStarted, Compaction: event.Compaction{Trigger: trigger}})

	if a.hooks != nil {
		if hookInstr := a.hooks.PreCompact(ctx, trigger); hookInstr != "" {
			if instructions != "" {
				instructions += "\n"
			}
			instructions += hookInstr
		}
	}

	var err error
	fold, instructions, err = a.interceptCompactionPrepare(ctx, fold, instructions)
	if err != nil {
		a.emitCompactionAborted(trigger)
		return CompactionNoop, err
	}
	if len(fold) == 0 {
		a.emitCompactionAborted(trigger)
		return CompactionNoop, nil
	}

	sourceTokens := a.estimatedPromptTokens(visibleInput)
	res, err := a.foldToSummary(ctx, fold, instructions)
	summary := res.Text
	tele := compactionTelemetryFromSummary(trigger, a.CacheState(), sourceTokens, res)
	if err != nil {
		tele.Error = err.Error()
		a.emitCompactionTelemetry(tele)
		a.emitCompactionAborted(trigger)
		return CompactionNoop, err
	}
	summary, err = a.interceptCompactionComplete(ctx, summary)
	if err != nil {
		tele.Error = err.Error()
		a.emitCompactionTelemetry(tele)
		a.emitCompactionAborted(trigger)
		return CompactionNoop, err
	}
	archived := ""

	projMsgs := make([]provider.Message, 0, head+len(early)+len(carried)+1+len(kept)+len(msgs)-start)
	projMsgs = append(projMsgs, msgs[:head]...)
	projMsgs = append(projMsgs, early...)
	projMsgs = append(projMsgs, carried...)
	projMsgs = append(projMsgs, formatSummaryMessage(summary))
	projMsgs = append(projMsgs, kept...)
	projMsgs = append(projMsgs, msgs[start:]...)
	projMsgs = provider.ModelMessages(projMsgs)

	projTokens := estimateMessagesTokens(a.providerProjectionMessages(projMsgs))
	tele.ProjectionTokens = projTokens
	a.emitCompactionTelemetry(tele)

	viewOutputHash := providerVisibleFingerprint(provider.ModelMessages(projMsgs))
	st, err := a.commitSummaryProjection(summaryProjectionCommit{
		canonical: canonical, fold: fold, projected: projMsgs, result: res,
		transcriptVersion: transcriptVersion, projectionVersion: startProjectionVersion,
		generation: startGeneration, activeTurn: activeTurn, trigger: trigger,
		summary: summary, inputHash: viewInputHash, outputHash: viewOutputHash,
		sourceTokens: sourceTokens, projectionTokens: projTokens,
	})
	if err != nil {
		a.emitCompactionAborted(trigger)
		return CompactionNoop, err
	}
	archived = st.LastReceipt.Archive

	a.sink.Emit(event.Event{Kind: event.CompactionDone, Compaction: event.Compaction{
		Trigger: trigger, Messages: len(fold), Summary: summary, Archive: archived,
	}})
	return CompactionInstalled, nil
}

// planFoldRegion locates msgs[head:start] for a fold, stopping short of an
// active turn so a tool loop is never folded mid-flight. ok is false when there
// is nothing left to fold.
func (a *Agent) planFoldRegion(msgs []provider.Message) (head, start int, ok bool) {
	head, start, ok = a.planCompaction(msgs, minCompactMessages)
	if !ok {
		head, start, ok = a.planCompaction(msgs, 1)
	}
	if !ok {
		return head, start, false
	}
	if active := a.activeTurnStart(msgs); active >= head && active < start {
		start = active
	}
	return head, start, start > head
}

// foldSource picks what a fold reads. By default every fold re-derives its
// digest from the canonical transcript, so digests never chain — at the cost of
// re-reading the whole session each time. The incremental experiment folds the
// model-visible view instead, which feeds the previous digest back through the
// summarizer: cheaper per fold, and lossy in a way CompactionBench measures.
func (a *Agent) foldSource(canonical []provider.Message) []provider.Message {
	if !a.ablation.Off(ablation.FullFold) {
		return canonical
	}
	if visible := a.modelVisibleMessages(); len(visible) > 0 {
		return visible
	}
	return canonical
}

// partitionFoldForProjection splits the fold region three ways: user turns
// hoisted verbatim ahead of the digest, messages the keep policy protects, and
// the remainder that folds (prior digests included, so a merge yields one
// summary). The groups partition the region — one pass decides each message
// once, so no turn can fall between a hoist rule and a fold rule that disagree.
func (a *Agent) partitionFoldForProjection(region []provider.Message) (early, carried, kept, fold []provider.Message) {
	policyKeep := keepIndexes(region, a.keepPolicy)
	hoist := a.earlyUserTurns(region)
	carryDigests := a.carryPriorDigests(region)
	for i, m := range region {
		switch {
		case m.LocalOnly: // display-only output never reaches a provider
		case hoist[i]:
			early = append(early, m)
		case isCompactionSummary(m):
			if carryDigests {
				carried = append(carried, m)
				continue
			}
			fold = append(fold, m)
		case policyKeep[i]:
			kept = append(kept, a.keptForProjection(m))
		default:
			fold = append(fold, m)
		}
	}
	return early, carried, kept, fold
}

// carryPriorDigests reports whether the digests already in the region survive
// this fold verbatim instead of being re-summarized. Re-summarizing a digest is
// the only step in a fold where a fact can be dropped and never recovered, so
// an incremental fold carries them — until they outgrow their budget, when one
// consolidating fold merges them and the chain starts over.
func (a *Agent) carryPriorDigests(region []provider.Message) bool {
	if !a.ablation.Off(ablation.FullFold) {
		return false
	}
	total := 0
	for _, m := range region {
		if isCompactionSummary(m) {
			total += summaryInputTokens([]provider.Message{m})
		}
	}
	if total > maxCarriedDigestTokens {
		a.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo, Text: fmt.Sprintf(
			"consolidating %d tokens est. of carried digests into one; earlier facts now depend on this merge", total)})
		return false
	}
	return true
}

// earlyUserTurns marks the region positions of the small user turns hoisted
// verbatim ahead of the digest. Selecting from the fold region alone keeps the
// set disjoint from the verbatim tail, and taking the first N (never the latest
// N) keeps the hoisted bytes identical across folds: the region only ever grows
// at its end, so the set can gain a member but never reorder or lose one.
func (a *Agent) earlyUserTurns(region []provider.Message) []bool {
	hoist := make([]bool, len(region))
	n := 0
	for i, m := range region {
		if n == maxEarlyUserTurns {
			break
		}
		if m.LocalOnly || m.Role != provider.RoleUser || isCompactionSummary(m) {
			continue
		}
		if !a.fixedPinnableUserTurn(m) {
			continue
		}
		hoist[i] = true
		n++
	}
	return hoist
}

// runCompactionSummary tries native compaction first, then summarizeWithRetry.
// On total failure it returns the error without a mechanical marker.
func (a *Agent) runCompactionSummary(ctx context.Context, fold []provider.Message, instructions string) (summary, mode string, usage *provider.Usage, providerReqID string, err error) {
	if nc, ok := provider.AsNativeCompactor(a.prov); ok {
		maxOut := 0
		if cc, ok := provider.AsCompactionCapabler(a.prov); ok {
			caps := cc.CompactionCapabilities()
			maxOut = caps.CompactionOutputTokens
			if maxOut <= 0 {
				maxOut = caps.MaxOutputTokens
			}
		}
		a.compactionMu.Lock()
		sessionPath := a.sessionPath
		a.compactionMu.Unlock()
		res, nerr := nc.Compact(ctx, provider.CompactionRequest{
			Messages:        fold,
			Instructions:    instructions,
			MaxOutputTokens: maxOut,
			PromptCacheKey:  promptCacheKey(a.workspaceID, BranchID(sessionPath), a.modelRef),
			SessionID:       BranchID(sessionPath),
		})
		if nerr == nil && res.Valid() {
			if res.Summary != "" {
				return res.Summary, CompactionModeNative, res.Usage, res.ProviderRequestID, nil
			}
			// Provider returned a full projection; extract summary text if present,
			// otherwise render the projection as the digest body.
			if s := extractLatestSummary(res.Projection); s != "" {
				return s, CompactionModeNative, res.Usage, res.ProviderRequestID, nil
			}
			return renderTranscript(res.Projection), CompactionModeNative, res.Usage, res.ProviderRequestID, nil
		}
		if nerr != nil && !errors.Is(nerr, provider.ErrCompactionUnsupported) {
			// Hard native failure: still try ordinary summarize fallback.
			a.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo, Text: "Native compaction unavailable; using summary fallback.", Detail: nerr.Error()})
		}
	}
	summary, usage, err = a.summarizeWithRetry(ctx, fold, instructions)
	if err != nil {
		return "", CompactionModeSummarized, usage, "", err
	}
	return summary, CompactionModeSummarized, usage, "", nil
}

// installPruneProjection stores a projection whose messages are a snipped/pruned
// view of the canonical transcript (no summarizer call).
func (a *Agent) installPruneProjection(view []provider.Message, st PruneStats) error {
	canonical, version := a.session.snapshotMessagesVersion()
	view = provider.ModelMessages(view)
	if len(view) == 0 {
		return nil
	}
	// The maintenance input must still be the current visible view. This is the
	// compare half of the two-phase projection transaction; a concurrent append,
	// model switch or newer maintenance pass causes a safe retry instead of a
	// last-writer-wins overwrite.
	a.compactionMu.Lock()
	current := a.compactionState
	a.compactionMu.Unlock()
	currentVisible := canonical
	if projectionValid(current, canonical, version, a.currentPromptCacheKey()) {
		if projected := modelVisibleFromProjection(current.Projection, canonical); len(projected) > 0 {
			currentVisible = projected
		}
	}
	currentInputHash := providerVisibleFingerprint(provider.ModelMessages(currentVisible))
	if st.InputHash != "" && currentInputHash != st.InputHash {
		return fmt.Errorf("context maintenance became stale")
	}
	src := estimateMessagesTokens(provider.ModelMessages(currentVisible))
	dst := estimateMessagesTokens(view)
	if src <= dst && st.Results > 0 {
		return nil
	}
	// Do not pay a cache-prefix break for a negligible rewrite while the view is
	// still inside the hard input ceiling. Near/over the ceiling this guard is
	// intentionally bypassed: safety takes precedence over cache preservation.
	if st.Results > 0 && !st.Force && src < a.hardInputCeiling() && src-dst < a.minimumMaintenanceSavingsTokens() {
		return nil
	}
	// Planning uses a stable placeholder and performs no I/O. Archive only after
	// the compare/min-savings gates prove this projection can be installed.
	if st.Results > 0 && a.archiveDir != "" {
		originals := make([]provider.Message, 0, st.Results)
		for i := range currentVisible {
			if i >= len(view) || currentVisible[i].Content == view[i].Content {
				continue
			}
			originals = append(originals, currentVisible[i])
		}
		if len(originals) > 0 {
			archive, err := archiveMessages(a.archiveDir, originals)
			if err != nil {
				return fmt.Errorf("archive: %w", err)
			}
			st.Archive = archive
			for i := range currentVisible {
				if i >= len(view) || currentVisible[i].Content == view[i].Content {
					continue
				}
				m := currentVisible[i]
				replacement, ok := a.maintenanceReplacement(m, st.Mode, archive)
				if !ok {
					continue
				}
				m.Content = replacement
				view[i] = m
			}
			dst = estimateMessagesTokens(view)
		}
	}
	projVersion := current.Projection.ProjectionVersion + 1
	inputHash := currentInputHash
	outputHash := providerVisibleFingerprint(view)
	operationID := fmt.Sprintf("%s-%d-%s", CompactionTriggerSnip, projVersion, outputHash)
	action := "snip"
	if st.Mode == toolResultPrune {
		action = "prune"
	}
	now := time.Now().UTC()
	state := CompactionState{
		SchemaVersion:                compactionStateSchemaCurrent,
		TranscriptVersion:            version,
		Generation:                   current.Generation + 1,
		NativeContextEditingAccepted: a.nativeContextEditingAccepted.Load(),
		ContextEditingFallbackLocal:  a.contextEditingRuntimeFallback.Load(),
		Projection: ContextProjection{
			Messages:          view,
			TranscriptVersion: version,
			ProjectionVersion: projVersion,
			CoveredCount:      len(canonical),
			CoveredPrefixHash: coveredPrefixHash(canonical, len(canonical)),
			SourceTokens:      src,
			ProjectionTokens:  dst,
			ViewInputHash:     inputHash,
			ViewOutputHash:    outputHash,
			CreatedAt:         now,
		},
		PromptCacheKey:   a.currentPromptCacheKey(),
		LastCacheState:   a.CacheState(),
		LastTrigger:      CompactionTriggerPressure,
		LastMode:         CompactionModeSnip,
		LastSourceTokens: src,
		LastResultTokens: dst,
		LastReceipt: &ContextMaintenanceReceipt{
			OperationID: operationID, Status: "applied", Action: action,
			Trigger: CompactionTriggerPressure, SourceProjection: current.Projection.ProjectionVersion,
			ProjectionVersion: projVersion,
			CoveredCount:      len(canonical), CoveredPrefixHash: coveredPrefixHash(canonical, len(canonical)),
			InputHash: inputHash, OutputHash: outputHash, InputTokens: src, ResultTokens: dst,
			SavedTokens: max(0, src-dst), AffectedToolResults: st.Results, Archive: st.Archive,
			CacheBreak: true, CreatedAt: now,
		},
		UpdatedAt: now,
	}
	if err := a.installProjectionIfCurrent(state, current.Projection.ProjectionVersion, current.Generation); err != nil {
		return err
	}
	a.emitContextMaintenance(state.LastReceipt)
	return nil
}
