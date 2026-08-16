import { useCallback, useEffect, useMemo, useRef, useState, type RefObject } from "react";
import type { TranscriptRow } from "./transcriptRows";
import { useTranscriptVirtuosoFirstItemIndex } from "./transcriptVirtuosoIndex";
import {
  captureTranscriptLayoutAnchor,
  transcriptAnchorInitialLocation,
  transcriptElementViewportIsBlank,
  type TranscriptLayoutAnchor,
} from "./transcriptVirtuosoRecovery";
import { resolveTranscriptStateSnapshot, type TranscriptStateSnapshot } from "./transcriptStateSnapshot";
import type { TranscriptScrollArbiterRecoveryApi } from "./useTranscriptScrollArbiter";

const BLANK_RECOVERY_COOLDOWN_MS = 2_000;
// A viewport that blanks while the user is actively scrolling is almost always
// a transient mount lag (slow renderer outrunning the fling), not a broken
// size tree. Resets wait for the scroll to go quiet; only a blank that
// survives into idle earns a rebuild.
const USER_SCROLL_IDLE_MS = 320;

/**
 * Detects a stale Virtuoso size tree and rebuilds it while preserving the
 * logical viewport. This hook never writes scrolls itself and never touches
 * the Virtuoso handle: anchor restores are submitted to the scroll arbiter
 * as recovery requests with an explicit terminal state (done / cancelled /
 * expired), and user scroll intent preempts them through the arbiter's own
 * intent events (#8657).
 *
 * Content patches (history_items_patch) never reach this hook: they update
 * the row data and Virtuoso re-measures the mounted rows itself, which is
 * local, incremental, and keeps the measured size tree intact. The only
 * remaining rebuild trigger is the blank-viewport watchdog, which fires when
 * the size tree is genuinely broken — not on a schedule derived from content
 * revisions.
 */
export function useTranscriptLayoutIntegrity({
  surfaceKey,
  rows,
  rowIndexByKey,
  scrollRef,
  pinnedRef,
  readyRef,
  scrollToBottom,
  submitRecoveryRequest,
  retryRecoveryRequest,
  lastGoodAnchorRef,
  captureStateSnapshot,
}: {
  surfaceKey: string;
  rows: readonly TranscriptRow[];
  rowIndexByKey: ReadonlyMap<string, number>;
  scrollRef: RefObject<HTMLDivElement | null>;
  pinnedRef: RefObject<boolean>;
  readyRef: RefObject<boolean>;
  scrollToBottom: () => void;
} & TranscriptScrollArbiterRecoveryApi) {
  const [resetEpoch, setResetEpoch] = useState(0);
  const blankCheckFrameRef = useRef<number | null>(null);
  const pendingAnchorRef = useRef<{ surfaceKey: string; anchor: TranscriptLayoutAnchor } | null>(null);
  const lastBlankRecoveryAtRef = useRef(0);
  const userScrollActiveRef = useRef(false);
  const userScrollIdleTimerRef = useRef<number | null>(null);
  const recoveryRetryTimerRef = useRef<number | null>(null);
  // A single rAF-pair blank sighting can still be mount lag; only two
  // consecutive idle blank checks earn a rebuild.
  const consecutiveBlankRef = useRef(0);
  const activeRecoveryIdRef = useRef<number | null>(null);
  const suspendedRecoveryIdRef = useRef<number | null>(null);
  const rowKeys = useMemo(() => rows.map((row) => String(row.key)), [rows]);
  const surfaceStateRef = useRef<{ surfaceKey: string; rowKeys: readonly string[] }>({ surfaceKey, rowKeys });
  const stateSnapshotRef = useRef<TranscriptStateSnapshot | null>(null);
  const appliedSnapshotRef = useRef(false);

  // Render-phase surface transition: the outgoing Virtuoso is still mounted
  // at this point (an effect cleanup would run after the keyed remount), so
  // this is the last chance to snapshot its measured tree + scrollTop for
  // the incoming surface to restore from.
  if (surfaceStateRef.current.surfaceKey !== surfaceKey) {
    const snapshot = captureStateSnapshot();
    if (snapshot && surfaceStateRef.current.rowKeys.length > 0) {
      stateSnapshotRef.current = { keys: surfaceStateRef.current.rowKeys, snapshot };
    }
    surfaceStateRef.current = { surfaceKey, rowKeys };
  } else {
    surfaceStateRef.current.rowKeys = rowKeys;
  }

  useEffect(() => {
    pendingAnchorRef.current = null;
    lastBlankRecoveryAtRef.current = 0;
    userScrollActiveRef.current = false;
    consecutiveBlankRef.current = 0;
    activeRecoveryIdRef.current = null;
    suspendedRecoveryIdRef.current = null;
    if (blankCheckFrameRef.current !== null) cancelAnimationFrame(blankCheckFrameRef.current);
    if (userScrollIdleTimerRef.current !== null) window.clearTimeout(userScrollIdleTimerRef.current);
    if (recoveryRetryTimerRef.current !== null) window.clearTimeout(recoveryRetryTimerRef.current);
    blankCheckFrameRef.current = null;
    userScrollIdleTimerRef.current = null;
    recoveryRetryTimerRef.current = null;
  }, [surfaceKey]);

  useEffect(() => () => {
    if (blankCheckFrameRef.current !== null) cancelAnimationFrame(blankCheckFrameRef.current);
    if (userScrollIdleTimerRef.current !== null) window.clearTimeout(userScrollIdleTimerRef.current);
    if (recoveryRetryTimerRef.current !== null) window.clearTimeout(recoveryRetryTimerRef.current);
  }, []);

  const requestReset = useCallback((): boolean => {
    const element = scrollRef.current;
    if (!element || pendingAnchorRef.current?.surfaceKey === surfaceKey) return false;
    // A blank viewport already lost its physical anchor. Restore the last
    // known-good position the arbiter tracked (a completed recovery or the
    // user's own resting position) instead of snapping the nearest mounted
    // row to the top, which systematically landed above the real position.
    const anchor = pinnedRef.current
      ? ({ mode: "tail" } as const)
      : lastGoodAnchorRef.current ?? captureTranscriptLayoutAnchor(element, false);
    if (!anchor) return false;
    // The watchdog only rebuilds after declaring the current size tree
    // broken. Never feed that same tree back through restoreStateFrom; the
    // measured-height cache plus the logical anchor rebuild it safely.
    stateSnapshotRef.current = null;
    pendingAnchorRef.current = { surfaceKey, anchor };
    readyRef.current = false;
    setResetEpoch((epoch) => epoch + 1);
    return true;
  }, [lastGoodAnchorRef, pinnedRef, readyRef, scrollRef, surfaceKey]);

  // Explicit user scroll intent outranks recovery: drop any pending anchor so
  // a later reset re-captures from the user's own position. An in-flight
  // recovery request is preempted separately by the arbiter's own intent
  // events (wheel/touch/key/pointer all dispatch through it) (#8657/#8688).
  const invalidateAnchors = useCallback(() => {
    pendingAnchorRef.current = null;
  }, []);

  const resetKey = `${surfaceKey}:${resetEpoch}`;
  const firstItemIndex = useTranscriptVirtuosoFirstItemIndex(rows, resetKey);
  const pendingAnchor = pendingAnchorRef.current?.surfaceKey === surfaceKey ? pendingAnchorRef.current.anchor : undefined;
  const restoreLocation = transcriptAnchorInitialLocation(pendingAnchor, rowIndexByKey, firstItemIndex);
  // A usable snapshot outranks restoreLocation: Virtuoso pipes restoreStateFrom
  // into the same initial-location stream, so the two never apply together.
  const restoreSnapshot = useMemo(
    () => resolveTranscriptStateSnapshot(stateSnapshotRef.current, rowKeys),
    // stateSnapshotRef only changes at the capture points above; every remount
    // path recomputes through one of these deps.
    [rowKeys, surfaceKey, resetEpoch],
  );
  appliedSnapshotRef.current = restoreSnapshot !== undefined;

  const captureUserAnchor = useCallback((): TranscriptLayoutAnchor | undefined => {
    const element = scrollRef.current;
    return element ? captureTranscriptLayoutAnchor(element, pinnedRef.current) : undefined;
  }, [pinnedRef, scrollRef]);

  const clearSuspendedRecovery = useCallback(() => {
    suspendedRecoveryIdRef.current = null;
    if (recoveryRetryTimerRef.current !== null) {
      window.clearTimeout(recoveryRetryTimerRef.current);
      recoveryRetryTimerRef.current = null;
    }
  }, []);

  const armSuspendedRecoveryRetry = useCallback((id: number) => {
    if (recoveryRetryTimerRef.current !== null) window.clearTimeout(recoveryRetryTimerRef.current);
    const retryWhenIdle = () => {
      recoveryRetryTimerRef.current = window.setTimeout(() => {
        recoveryRetryTimerRef.current = null;
        if (suspendedRecoveryIdRef.current !== id) return;
        if (userScrollActiveRef.current) {
          retryWhenIdle();
          return;
        }
        suspendedRecoveryIdRef.current = null;
        retryRecoveryRequest(id);
      }, USER_SCROLL_IDLE_MS);
    };
    retryWhenIdle();
  }, [retryRecoveryRequest]);

  // Hands the pending rebuild anchor to the arbiter, which owns the actual
  // aim/settle writes and the request's terminal state from here on.
  const submitAnchorRecovery = useCallback((anchor: TranscriptLayoutAnchor) => {
    if (pendingAnchorRef.current?.surfaceKey !== surfaceKey) return;
    pendingAnchorRef.current = null;
    const id = submitRecoveryRequest({
      anchor,
      locate: (current) => transcriptAnchorInitialLocation(current, rowIndexByKey, firstItemIndex),
      captureUserAnchor,
      onSettle: () => {
        activeRecoveryIdRef.current = null;
        clearSuspendedRecovery();
      },
      // On user-takeover the arbiter already recorded the user's viewport
      // anchor as the new lastGoodAnchor; the integrity side simply stops.
      onCancel: () => {
        activeRecoveryIdRef.current = null;
        clearSuspendedRecovery();
      },
      onSuspend: (suspendedId) => {
        suspendedRecoveryIdRef.current = suspendedId;
        armSuspendedRecoveryRetry(suspendedId);
      },
      onExpired: () => {
        activeRecoveryIdRef.current = null;
        clearSuspendedRecovery();
      },
    });
    activeRecoveryIdRef.current = id;
  }, [armSuspendedRecoveryRetry, captureUserAnchor, clearSuspendedRecovery, firstItemIndex, rowIndexByKey, submitRecoveryRequest, surfaceKey]);

  const scheduleBlankViewportCheck = useCallback(() => {
    if (
      blankCheckFrameRef.current !== null
      || pendingAnchorRef.current?.surfaceKey === surfaceKey
      || activeRecoveryIdRef.current !== null
    ) return;
    blankCheckFrameRef.current = requestAnimationFrame(() => {
      blankCheckFrameRef.current = requestAnimationFrame(() => {
        blankCheckFrameRef.current = null;
        if (userScrollActiveRef.current) return;
        const element = scrollRef.current;
        if (!element || !transcriptElementViewportIsBlank(element)) {
          consecutiveBlankRef.current = 0;
          return;
        }
        consecutiveBlankRef.current += 1;
        if (consecutiveBlankRef.current < 2) return;
        consecutiveBlankRef.current = 0;
        // Cooldown-only dedup: content revisions no longer reach this hook,
        // so they cannot rotate the dedup key mid-storm. A blank that
        // persists past the cooldown earns another rebuild.
        const now = Date.now();
        if (now - lastBlankRecoveryAtRef.current < BLANK_RECOVERY_COOLDOWN_MS) return;
        lastBlankRecoveryAtRef.current = now;
        requestReset();
      });
    });
  }, [requestReset, scrollRef, surfaceKey]);

  // Runs when user-driven scrolling has been quiet for USER_SCROLL_IDLE_MS.
  // Suspended recoveries own a separate bounded retry timer so they cannot
  // wait forever for user input; this lane only re-checks the viewport.
  const handleUserScrollIdle = useCallback(() => {
    userScrollActiveRef.current = false;
    scheduleBlankViewportCheck();
  }, [scheduleBlankViewportCheck]);

  const armUserScrollIdleTimer = useCallback(() => {
    if (userScrollIdleTimerRef.current !== null) window.clearTimeout(userScrollIdleTimerRef.current);
    userScrollIdleTimerRef.current = window.setTimeout(() => {
      userScrollIdleTimerRef.current = null;
      handleUserScrollIdle();
    }, USER_SCROLL_IDLE_MS);
  }, [handleUserScrollIdle]);

  // Wheel/touch/keyboard/pointer intent is explicit user scroll intent: it
  // aborts any in-flight recovery restore (user intent > recovery) and gates
  // blank checks until the scroll goes quiet (#8657/#8688 follow-up).
  const noteUserScrollIntent = useCallback(() => {
    userScrollActiveRef.current = true;
    invalidateAnchors();
    armUserScrollIdleTimer();
  }, [armUserScrollIdleTimer, invalidateAnchors]);

  // Scroll events after an intent keep the active window alive; programmatic
  // scrolls (tail follow, restores) never arm it on their own.
  const noteScrollActivity = useCallback(() => {
    if (userScrollActiveRef.current) armUserScrollIdleTimer();
  }, [armUserScrollIdleTimer]);

  const handleItemsRendered = useCallback((renderedCount: number) => {
    if (!readyRef.current && renderedCount > 0) {
      readyRef.current = true;
      const pending = pendingAnchorRef.current;
      if (pending?.surfaceKey === surfaceKey) submitAnchorRecovery(pending.anchor);
      // A restored snapshot already placed the view; the first-mount
      // scrollToBottom would yank it straight back to the tail.
      else if (!appliedSnapshotRef.current) requestAnimationFrame(scrollToBottom);
    }
    scheduleBlankViewportCheck();
  }, [readyRef, scheduleBlankViewportCheck, scrollToBottom, submitAnchorRecovery, surfaceKey]);

  return {
    resetKey,
    firstItemIndex,
    restoreLocation,
    restoreSnapshot,
    handleItemsRendered,
    scheduleBlankViewportCheck,
    invalidateAnchors,
    noteUserScrollIntent,
    noteScrollActivity,
  };
}
