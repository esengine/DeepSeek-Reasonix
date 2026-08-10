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
	promptCacheKey    string
}

func (a *Agent) snapshotExplicitCompression() explicitCompressionSnapshot {
	canonical, version := a.session.snapshotMessagesVersion()
	cacheKey := a.currentPromptCacheKey()
	state := a.compactionState
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
	archive      string
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

	if err := a.installVisibleCompression(snap, trigger, res.Mode, summary, projection, result.SourceTokens, projectionTokens, res.Usage); err != nil {
		if errors.Is(err, errCompressStaleContext) {
			tele.Error = err.Error()
			a.emitCompactionTelemetry(tele)
		}
		a.emitCompactionAborted(trigger)
		return tool.CompressResult{}, err
	}
	a.session.NoteContentRewrite("compact_" + trigger)
	a.emitCompactionTelemetry(tele)
	a.sink.Emit(event.Event{Kind: event.CompactionDone, Compaction: event.Compaction{
		Trigger: trigger, Messages: len(plan.fold), Summary: summary, Archive: prepared.archive,
	}})
	result.Status = "ok"
	result.Reason = ""
	return result, nil
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
	prepared := preparedVisibleCompression{fold: preparedFold, instructions: preparedInstructions}
	if a.archiveDir == "" {
		return prepared, "", nil
	}
	prepared.archive, err = archiveMessages(a.archiveDir, preparedFold)
	if err != nil {
		return preparedVisibleCompression{}, "", fmt.Errorf("archive: %w", err)
	}
	return prepared, "", nil
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

func (a *Agent) installVisibleCompression(snap explicitCompressionSnapshot, trigger, mode, summary string, projection []provider.Message, sourceTokens, projectionTokens int, usage *provider.Usage) error {
	current, currentVersion := a.session.snapshotMessagesVersion()
	if currentVersion != snap.transcriptVersion || len(current) != len(snap.canonical) ||
		coveredPrefixHash(current, len(current)) != snap.coveredHash ||
		a.compactionState.Projection.ProjectionVersion != snap.projectionVersion {
		return errCompressStaleContext
	}
	now := time.Now().UTC()
	state := CompactionState{
		SchemaVersion:     compactionStateSchemaCurrent,
		TranscriptVersion: snap.transcriptVersion,
		Projection: ContextProjection{
			Messages:          projection,
			TranscriptVersion: snap.transcriptVersion,
			ProjectionVersion: snap.projectionVersion + 1,
			CoveredCount:      len(snap.canonical),
			CoveredPrefixHash: snap.coveredHash,
			SummaryHash:       summaryContentHash(summary),
			SourceTokens:      sourceTokens,
			ProjectionTokens:  projectionTokens,
			CreatedAt:         now,
		},
		PromptCacheKey:   snap.promptCacheKey,
		LastCacheState:   a.CacheState(),
		LastTrigger:      trigger,
		LastMode:         mode,
		LastSourceTokens: sourceTokens,
		LastResultTokens: projectionTokens,
		UpdatedAt:        now,
	}
	if a.pricing != nil && usage != nil {
		state.LastCompactionCost = a.pricing.Cost(usage)
	}
	if err := a.installProjection(state); err != nil {
		return fmt.Errorf("persist projection: %w", err)
	}
	return nil
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
	canonical, transcriptVersion := a.session.snapshotMessagesVersion()
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

	archived := ""
	if a.archiveDir != "" {
		path, aerr := archiveMessages(a.archiveDir, fold)
		if aerr != nil {
			a.emitCompactionAborted(trigger)
			return CompactionNoop, fmt.Errorf("archive: %w", aerr)
		}
		archived = path
	}

	sourceTokens := estimateMessagesTokens(provider.ModelMessages(canonical))
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

	projVersion := a.compactionState.Projection.ProjectionVersion + 1
	st := CompactionState{
		SchemaVersion:     compactionStateSchemaCurrent,
		TranscriptVersion: transcriptVersion,
		Projection: ContextProjection{
			Messages:          projMsgs,
			TranscriptVersion: transcriptVersion,
			ProjectionVersion: projVersion,
			CoveredCount:      len(canonical),
			CoveredPrefixHash: coveredPrefixHash(canonical, len(canonical)),
			SummaryHash:       summaryContentHash(summary),
			SourceTokens:      sourceTokens,
			ProjectionTokens:  projTokens,
			CreatedAt:         time.Now().UTC(),
		},
		PromptCacheKey:   a.currentPromptCacheKey(),
		LastCacheState:   a.CacheState(),
		LastTrigger:      trigger,
		LastMode:         res.Mode,
		LastSourceTokens: sourceTokens,
		LastResultTokens: projTokens,
		UpdatedAt:        time.Now().UTC(),
	}
	if a.pricing != nil && res.Usage != nil {
		st.LastCompactionCost = a.pricing.Cost(res.Usage)
	}
	if err := a.installProjection(st); err != nil {
		a.emitCompactionAborted(trigger)
		return CompactionNoop, fmt.Errorf("persist projection: %w", err)
	}
	a.session.NoteContentRewrite("compact_" + trigger)

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
			kept = append(kept, m)
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
		res, nerr := nc.Compact(ctx, provider.CompactionRequest{
			Messages:        fold,
			Instructions:    instructions,
			MaxOutputTokens: maxOut,
			PromptCacheKey:  promptCacheKey(a.workspaceID, BranchID(a.sessionPath), a.modelRef),
			SessionID:       BranchID(a.sessionPath),
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

// snipToProjection builds a projection that only snips stale tool results.
func (a *Agent) snipToProjection(ctx context.Context) error {
	_ = ctx
	msgs, _ := a.session.snapshotMessagesVersion()
	snipped, st := a.applyToolResultMaintenanceView(msgs, toolResultSnip)
	if st.Results == 0 {
		return nil
	}
	ratio := a.tokPerChar()
	saved := int(float64(st.SavedChars) * ratio)
	a.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo, Text: fmt.Sprintf(
		"snipped %d stale tool results (~%d tokens est.) before compaction", st.Results, saved)})
	return a.installPruneProjection(snipped, st)
}

// installPruneProjection stores a projection whose messages are a snipped/pruned
// view of the canonical transcript (no summarizer call).
func (a *Agent) installPruneProjection(view []provider.Message, st PruneStats) error {
	msgs, version := a.session.snapshotMessagesVersion()
	view = provider.ModelMessages(view)
	src := estimateMessagesTokens(provider.ModelMessages(msgs))
	dst := estimateMessagesTokens(view)
	projVersion := a.compactionState.Projection.ProjectionVersion + 1
	state := CompactionState{
		SchemaVersion:     compactionStateSchemaCurrent,
		TranscriptVersion: version,
		Projection: ContextProjection{
			Messages:          view,
			TranscriptVersion: version,
			ProjectionVersion: projVersion,
			CoveredCount:      len(msgs),
			CoveredPrefixHash: coveredPrefixHash(msgs, len(msgs)),
			SourceTokens:      src,
			ProjectionTokens:  dst,
			CreatedAt:         time.Now().UTC(),
		},
		PromptCacheKey:   a.currentPromptCacheKey(),
		LastCacheState:   a.CacheState(),
		LastTrigger:      CompactionTriggerSnip,
		LastMode:         CompactionModeSnip,
		LastSourceTokens: src,
		LastResultTokens: dst,
		UpdatedAt:        time.Now().UTC(),
	}
	_ = st
	return a.installProjection(state)
}

// emitCompactionTelemetry records structured compaction observability without
// logging sensitive transcript content.
func (a *Agent) emitCompactionTelemetry(t CompactionTelemetry) {
	detail := fmt.Sprintf("trigger=%s mode=%s cache=%s src=%d fold=%d spans=%d proj=%d in=%d out=%d hit=%d miss=%d write=%d reqs=%d",
		t.Trigger, t.Mode, t.CacheState, t.SourceTokens, t.FoldTokens, t.Spans, t.ProjectionTokens,
		t.InputTokens, t.OutputTokens, t.CacheHitTokens, t.CacheMissTokens, t.CacheWriteTokens, t.RequestCount)
	if t.ProviderRequestID != "" {
		detail += " provider_request_id=" + t.ProviderRequestID
	}
	if t.Error != "" {
		detail += " err_type=" + t.Error
	}
	level := event.LevelInfo
	text := "compaction telemetry"
	if t.Error != "" {
		level = event.LevelWarn
		text = "compaction failed"
	}
	a.sink.Emit(event.Event{Kind: event.Notice, Level: level, Text: text, Detail: detail})
}

// emitCompactionAborted resolves a "compacting…" placeholder when a pass fails
// after the Started event: a Done with no summary tells a frontend to drop the
// placeholder. The caller still surfaces the reason (a Notice), so this carries
// no text of its own.
func (a *Agent) emitCompactionAborted(trigger string) {
	a.sink.Emit(event.Event{Kind: event.CompactionDone, Compaction: event.Compaction{Trigger: trigger}})
}
