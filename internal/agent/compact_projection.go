package agent

import (
	"context"
	"errors"
	"fmt"
	"time"

	"reasonix/internal/event"
	"reasonix/internal/provider"
)

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
	msgs, transcriptVersion := a.session.snapshotMessagesVersion()
	head, start, ok := a.planCompaction(msgs, minCompactMessages)
	if !ok {
		head, start, ok = a.planCompaction(msgs, 1)
	}
	if !ok {
		return CompactionNoop, nil
	}
	if active := a.activeTurnStart(msgs); active >= head && active < start {
		start = active
		if start <= head {
			return CompactionNoop, nil
		}
	}
	region := msgs[head:start]
	kept, fold := a.partitionFoldForProjection(region)
	if len(fold) == 0 {
		return CompactionNoop, nil
	}
	if !force && !foldEconomics(fold) {
		return CompactionNoop, nil
	}

	a.sink.Emit(event.Event{Kind: event.CompactionStarted, Compaction: event.Compaction{Trigger: trigger},
		Detail: fmt.Sprintf("folding %d messages (kept %d verbatim)", len(fold), len(kept))})

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

	sourceTokens := estimateMessagesTokens(provider.ModelMessages(msgs))
	summary, mode, usage, providerReqID, err := a.runCompactionSummary(ctx, fold, instructions)
	tele := CompactionTelemetry{
		Trigger:           trigger,
		CacheState:        a.CacheState(),
		Mode:              mode,
		Native:            mode == CompactionModeNative,
		SourceTokens:      sourceTokens,
		ProviderRequestID: providerReqID,
	}
	if usage != nil {
		tele.InputTokens = usage.PromptTokens
		tele.OutputTokens = usage.CompletionTokens
		tele.CacheHitTokens = usage.CacheHitTokens
		tele.CacheMissTokens = usage.CacheMissTokens
		tele.CacheWriteTokens = usage.CacheWriteTokens
		tele.RequestCount = usage.RequestCount
		if tele.RequestCount <= 0 {
			tele.RequestCount = 1
		}
	}
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

	// OSWorld 2.0 implicit-state injection: if the StateTracker has accumulated
	// implicit facts (recovered file paths, inferred IDs, unexplored sources)
	// and the model-generated summary lacks the "Hidden state" section, append
	// the tracker's snapshot so compaction does not lose recovered context.
	// This is the runtime defense that pairs with implicitStateLossWarning:
	// instead of only warning when state is lost, we proactively inject it.
	if a.longHorizon && a.stateTracker != nil {
		if snapshot := a.stateTracker.SnapshotImplicitState(); snapshot != "" {
			if sectionContentChars(summary, "Hidden state & recovered facts") <= 0 {
				summary += "\n\n## Hidden state & recovered facts (injected by StateTracker)\n" + snapshot
				a.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo, Text: "Implicit state injected into compaction summary.", Detail: fmt.Sprintf("StateTracker provided %d bytes of recovered facts that the model-generated summary omitted.", len(snapshot))})
			}
		}
	}

	// OSWorld 2.0 Navigator kernel: when the navigator is wired, its
	// implicit-state digest is also injected. The navigator maintains state
	// independently of the prompt (in its own state graph), so its facts
	// survive even a mid-task crash. This is a superset of StateTracker —
	// when both are present, both digests are merged so no recovered fact is
	// lost. If the section already exists (from StateTracker above), the
	// navigator's facts are appended to it rather than creating a duplicate.
	if a.longHorizon && a.navigator != nil {
		if navDigest := a.navigator.ImplicitStateDigest(); navDigest != "" {
			chars := sectionContentChars(summary, "Hidden state & recovered facts")
			if chars <= 0 {
				summary += "\n\n## Hidden state & recovered facts (injected by Navigator)\n" + navDigest
			} else {
				// Section already exists (from StateTracker); append the
				// navigator's facts to it so both sources are preserved.
				summary += "\n\n" + navDigest
			}
			a.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo, Text: "Navigator implicit state injected into compaction summary.", Detail: fmt.Sprintf("Navigator provided %d bytes of recovered facts (merged with existing section: %d chars).", len(navDigest), chars)})
		}
	}

	early := a.fixedEarlyUserTurns(msgs, head)
	projMsgs := make([]provider.Message, 0, head+len(early)+1+len(kept)+len(msgs)-start)
	projMsgs = append(projMsgs, msgs[:head]...)
	projMsgs = append(projMsgs, early...)
	projMsgs = append(projMsgs, formatSummaryMessage(summary))
	projMsgs = append(projMsgs, kept...)
	projMsgs = append(projMsgs, msgs[start:]...)
	projMsgs = provider.ModelMessages(projMsgs)
	if a.strictAlternatingRoles {
		projMsgs = coalesceProjectionUserRuns(projMsgs)
	}

	projTokens := estimateMessagesTokens(projMsgs)
	tele.ProjectionTokens = projTokens
	a.emitCompactionTelemetry(tele)

	projVersion := a.compactionState.Projection.ProjectionVersion + 1
	st := CompactionState{
		SchemaVersion:     compactionStateSchemaV1,
		TranscriptVersion: transcriptVersion,
		Projection: ContextProjection{
			Messages:          projMsgs,
			TranscriptVersion: transcriptVersion,
			ProjectionVersion: projVersion,
			CoveredCount:      len(msgs),
			CoveredPrefixHash: coveredPrefixHash(msgs, len(msgs)),
			SummaryHash:       summaryContentHash(summary),
			SourceTokens:      sourceTokens,
			ProjectionTokens:  projTokens,
			CreatedAt:         time.Now().UTC(),
		},
		PromptCacheKey:   a.currentPromptCacheKey(),
		LastCacheState:   a.CacheState(),
		LastTrigger:      trigger,
		LastMode:         mode,
		LastSourceTokens: sourceTokens,
		LastResultTokens: projTokens,
		UpdatedAt:        time.Now().UTC(),
	}
	if a.pricing != nil && usage != nil {
		st.LastCompactionCost = a.pricing.Cost(usage)
	}
	if err := a.installProjection(st); err != nil {
		a.emitCompactionAborted(trigger)
		return CompactionNoop, fmt.Errorf("persist projection: %w", err)
	}
	a.session.NoteContentRewrite("compact_" + trigger)

	a.sink.Emit(event.Event{Kind: event.CompactionDone, Compaction: event.Compaction{
		Trigger: trigger, Messages: len(fold), Summary: summary, Archive: archived,
	}, Detail: compactionDoneDetail(summary)})

	// Implicit-state loss detector: check that the 3 OSWorld 2.0-informed
	// sections are present with real content in model-generated summaries. A
	// missing or empty-header section means the summarizer skipped it —
	// implicit state was likely lost to the fold. Emit a LevelWarn so the
	// user can see at runtime when compaction dropped recoverable state.
	if warning := implicitStateLossWarning(summary); warning != "" {
		a.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelWarn, Text: warning})
	}
	return CompactionInstalled, nil
}

// partitionFoldForProjection is like partitionFold but prior digests join the
// fold so A1 rolling merge produces a single latest summary. Fixed early user
// turns are excluded from both kept and fold — the caller re-inserts them from
// the full transcript so their bytes stay position-stable.
func (a *Agent) partitionFoldForProjection(region []provider.Message) (kept, fold []provider.Message) {
	policyKeep := keepIndexes(region, a.keepPolicy)
	earlySeen := 0
	const maxEarly = 3
	for i, m := range region {
		if m.LocalOnly {
			continue
		}
		// Skip the fixed early small user turns — they are re-added from the
		// full transcript after the summary so the prefix stays byte-stable.
		if m.Role == provider.RoleUser && !isCompactionSummary(m) && a.fixedPinnableUserTurn(m) && earlySeen < maxEarly {
			earlySeen++
			continue
		}
		if isCompactionSummary(m) {
			fold = append(fold, m)
			continue
		}
		if policyKeep[i] {
			kept = append(kept, m)
			continue
		}
		if m.Role == provider.RoleUser && a.fixedPinnableUserTurn(m) {
			// Additional small user turns beyond the fixed early window fold so
			// the projection does not grow unbounded with every user fact.
			fold = append(fold, m)
			continue
		}
		fold = append(fold, m)
	}
	return kept, fold
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
		SchemaVersion:     compactionStateSchemaV1,
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
	detail := fmt.Sprintf("trigger=%s mode=%s cache=%s src=%d proj=%d in=%d out=%d hit=%d miss=%d write=%d reqs=%d",
		t.Trigger, t.Mode, t.CacheState, t.SourceTokens, t.ProjectionTokens,
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
