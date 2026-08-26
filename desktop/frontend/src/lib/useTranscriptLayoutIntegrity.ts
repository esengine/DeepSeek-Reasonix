import { useCallback, useEffect, useMemo, useRef, useState, type RefObject } from "react";
import type { TranscriptRow } from "./transcriptRows";
import { useTranscriptVirtuosoFirstItemIndex } from "./transcriptVirtuosoIndex";
import {
  captureTranscriptLayoutAnchor,
  transcriptAnchorInitialLocation,
  transcriptElementViewportIsBlank,
  type TranscriptLayoutAnchor,
} from "./transcriptVirtuosoRecovery";
import { createTranscriptStateGeometry, resolveTranscriptStateSnapshot, type TranscriptStateSnapshot } from "./transcriptStateSnapshot";
import type { TranscriptScrollArbiterRecoveryApi } from "./useTranscriptScrollArbiter";
import { recordTranscriptScrollDiagnostic } from "./transcriptScrollProbe";
import { isFrontendDiagnosticsBuild } from "./frontendDiagnosticsBuild";
import type { TranscriptGeometryEnvironment } from "./transcriptRowGeometry";

const BLANK_RECOVERY_COOLDOWN_MS = 2_000;
// A viewport that blanks while the user is actively scrolling is almost always
// a transient mount lag (slow renderer outrunning the fling), not a broken
// size tree. Resets wait for the scroll to go quiet; only a blank that
// survives into idle earns a rebuild.
const USER_SCROLL_IDLE_MS = 320;
const CAPTURE_SCROLL_DIAGNOSTICS = isFrontendDiagnosticsBuild(
  typeof __BUILD_CHANNEL__ === "string" ? __BUILD_CHANNEL__ : "development",
  Boolean(import.meta.env?.DEV),
);

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
  layoutTransientRef,
  layoutWidth,
  geometrySessionKey = surfaceKey,
  geometryEnvironment = { contentWidth: layoutWidth, typographySignature: "legacy" },
}: {
  surfaceKey: string;
  rows: readonly TranscriptRow[];
  rowIndexByKey: ReadonlyMap<string, number>;
  scrollRef: RefObject<HTMLDivElement | null>;
  pinnedRef: RefObject<boolean>;
  readyRef: RefObject<boolean>;
  scrollToBottom: () => void;
  layoutTransientRef: RefObject<boolean>;
  layoutWidth?: number;
  geometrySessionKey?: string;
  geometryEnvironment?: TranscriptGeometryEnvironment;
} & Omit<TranscriptScrollArbiterRecoveryApi, "captureStateSnapshot">) {
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
  const stateGeometry = useMemo(
    () => createTranscriptStateGeometry(geometrySessionKey, rows, geometryEnvironment),
    [geometryEnvironment, geometrySessionKey, rows],
  );
  const layoutGeneration = useMemo(
    () => `${surfaceKey}:${String(layoutWidth ?? "unknown")}:${rowKeys.join("\u0000")}`,
    [layoutWidth, rowKeys, surfaceKey],
  );
  // [generation, stage (0=unused, 1=reset, 2=probe)]
  const recoveryBudgetRef = useRef<[string, number]>([layoutGeneration, 0]);
  if (recoveryBudgetRef.current[0] !== layoutGeneration) {
    recoveryBudgetRef.current = [layoutGeneration, 0];
  }
  const surfaceStateRef = useRef<{ surfaceKey: string; rowKeys: readonly string[] }>({ surfaceKey, rowKeys });
  const stateSnapshotRef = useRef<TranscriptStateSnapshot | null>(null);
  const appliedSnapshotRef = useRef(false);

  // A surface transition is a product-level view reset. Measurements may be
  // reused through the session geometry LRU, but an outgoing scrollTop never
  // crosses the transition — even if another session happens to have the same
  // row keys. Invalidate readiness synchronously during render: a newly keyed
  // Virtuoso child can publish its first items from a layout effect before the
  // parent Transcript layout effect runs. Letting it inherit the old surface's
  // ready=true would skip the incoming surface's only initial tail placement.
  // Blank-watchdog rebuilds also reject their broken size tree.
  if (surfaceStateRef.current.surfaceKey !== surfaceKey) {
    readyRef.current = false;
    stateSnapshotRef.current = null;
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

  const requestReset = useCallback(() => {
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
    if (CAPTURE_SCROLL_DIAGNOSTICS) recordTranscriptScrollDiagnostic("blank-reset", { blank: true });
    setResetEpoch((epoch) => Math.abs(epoch) + 1);
    return true;
  }, [lastGoodAnchorRef, pinnedRef, readyRef, scrollRef, surfaceKey]);

  // Explicit user scroll intent outranks recovery: drop any pending anchor so
  // a later reset re-captures from the user's own position. An in-flight
  // recovery request is preempted separately by the arbiter's own intent
  // events (wheel/touch/key/pointer all dispatch through it) (#8657/#8688).
  const invalidateAnchors = useCallback(() => {
    pendingAnchorRef.current = null;
  }, []);

  const resetKey = `${surfaceKey}:${Math.abs(resetEpoch)}`;
  const safeMode = resetEpoch < 0 && recoveryBudgetRef.current[1] === 2;
  const firstItemIndex = useTranscriptVirtuosoFirstItemIndex(rows, resetKey);
  const pendingAnchor = pendingAnchorRef.current?.surfaceKey === surfaceKey ? pendingAnchorRef.current.anchor : undefined;
  const restoreLocation = transcriptAnchorInitialLocation(pendingAnchor, rowIndexByKey, firstItemIndex);
  // A usable snapshot outranks restoreLocation: Virtuoso pipes restoreStateFrom
  // into the same initial-location stream, so the two never apply together.
  const restoreSnapshot = useMemo(
    () => resolveTranscriptStateSnapshot(stateSnapshotRef.current, rowKeys, stateGeometry),
    // stateSnapshotRef only changes at the capture points above; every remount
    // path recomputes through one of these deps.
    [rowKeys, stateGeometry, surfaceKey, resetEpoch],
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
      || layoutTransientRef.current
    ) return;
    blankCheckFrameRef.current = requestAnimationFrame(() => {
      blankCheckFrameRef.current = requestAnimationFrame(() => {
        blankCheckFrameRef.current = null;
        if (recoveryBudgetRef.current[0] !== layoutGeneration) {
          consecutiveBlankRef.current = 0;
          return;
        }
        if (userScrollActiveRef.current || layoutTransientRef.current) {
          consecutiveBlankRef.current = 0;
          return;
        }
        const element = scrollRef.current;
        if (!element) return;
        const blank = transcriptElementViewportIsBlank(element);
        if (CAPTURE_SCROLL_DIAGNOSTICS) recordTranscriptScrollDiagnostic("blank-check", { blank });
        if (!blank) {
          consecutiveBlankRef.current = 0;
          // The bounded measurement probe is a repair transaction, not a
          // permanent virtualization mode. It exits without changing the
          // keyed Virtuoso generation once the viewport is healthy.
          if (safeMode) {
            setResetEpoch((epoch) => Math.abs(epoch));
          }
          return;
        }
        // This check is the probe's own post-render verdict. The generation
        // already spent its two-sighting mount-lag guard before entering safe
        // mode, so one still-blank result is enough to end the bounded probe.
        // Do not depend on Virtuoso emitting another itemsRendered/scroll
        // callback when the expanded range fails to mount useful coverage.
        if (safeMode) {
          consecutiveBlankRef.current = 0;
          setResetEpoch((epoch) => Math.abs(epoch));
          return;
        }
        consecutiveBlankRef.current += 1;
        if (consecutiveBlankRef.current < 2) return;
        consecutiveBlankRef.current = 0;
        // Content-only revisions never rotate layoutGeneration, so patch
        // storms cannot replenish this budget. Only a surface, row-structure,
        // or width change earns one hard remount and one bounded probe.
        const budget = recoveryBudgetRef.current;
        if (budget[1] > 0) {
          if (budget[1] === 1) {
            budget[1] = 2;
            setResetEpoch((epoch) => -Math.abs(epoch));
            return;
          }
          if (safeMode) setResetEpoch((epoch) => Math.abs(epoch));
          return;
        }
        const now = Date.now();
        if (now - lastBlankRecoveryAtRef.current < BLANK_RECOVERY_COOLDOWN_MS) return;
        if (requestReset()) {
          lastBlankRecoveryAtRef.current = now;
          budget[1] = 1;
        }
      });
    });
  }, [layoutGeneration, layoutTransientRef, requestReset, safeMode, scrollRef, surfaceKey]);

  // A probe is a one-shot transaction. Schedule its verdict from the hook so
  // both success and failure leave the larger (but bounded) overscan even if
  // Virtuoso emits no follow-up range or scroll event.
  useEffect(() => {
    if (!safeMode) return;
    const frame = requestAnimationFrame(() => scheduleBlankViewportCheck());
    return () => cancelAnimationFrame(frame);
  }, [safeMode, scheduleBlankViewportCheck]);

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
    safeMode,
  };
}
