import { lazy, Suspense, type CSSProperties, type ReactNode, useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState, useSyncExternalStore } from "react";
import { Virtuoso } from "react-virtuoso";
import type { ControllerLiveStore, HistoryLoadTrigger, HistoryMutation, Item, LiveStream } from "../lib/useController";
import type { CheckpointMeta, WireCompletionSummary } from "../lib/types";
import type { InvocationMetadataMap } from "../lib/invocationDisplay";
import { useT } from "../lib/i18n";
import { InvocationMetadataContext, TurnActions, UserMessage } from "./Message";
import { ToolCard } from "./ToolCard";
import { ExtensionCard } from "./ExtensionCard";
import { ArrowDown, Loader2 } from "lucide-react";
import { Welcome } from "./Welcome";
import { ReadOnlyBatch } from "./ReadOnlyBatch";
import { ToolGroup } from "./ToolGroup";
import { getProcessFoldPreference, onProcessFoldPreferenceChange, type ProcessFoldPreference } from "../lib/processFoldPreference";
import { isSteerNoticeText } from "../lib/useController";
import { useTranscriptEntranceAnimation } from "../lib/useEntranceAnimation";
import { useTranscriptSelectionRetention } from "../lib/useTranscriptSelectionRetention";
import {
  questionAnchorId,
} from "../lib/transcriptGrouping";
import {
  buildTranscriptRows,
  buildTurnModels,
  foldMapWithReasoningOpen,
  foldMapWithToggle,
  foldSegmentStates,
  reconcileFoldEntries,
  EMPTY_FOLDS,
  NO_LIVE,
  type FoldMap,
  type ToolItem,
  type TranscriptLiveFlags,
  type TranscriptRow,
  transcriptRowMeasurementVersion,
} from "../lib/transcriptRows";
import { assistantAnswerOnly } from "../lib/transcriptLiveTurn";
import { useTranscriptLiveTurnStability } from "../lib/useTranscriptLiveTurnStability";
import { createTranscriptMeasuredSizes, type TranscriptSynthesizedSizes } from "../lib/transcriptMeasuredSizes";
import {
  transcriptRowLayoutVariant,
  type TranscriptEstimateSource,
  type TranscriptGeometryEnvironment,
  type TranscriptRowLayoutVariant,
} from "../lib/transcriptRowGeometry";
import { readTranscriptGeometryEnvironment } from "../lib/transcriptGeometryEnvironment";
import { onTypographyPreferencesChange } from "../lib/typographyPreferences";
import { acquireMarkdownWorkerClient, releaseMarkdownWorkerClient } from "../lib/markdownWorkerClient";
import { noteTranscriptRecoveryTerminal } from "../lib/sessionDiagnostics";
import { useReasoningDisplayMode } from "../lib/reasoningDisplayPreference";
import { InlineAssistantReasoning } from "./InlineAssistantReasoning";
import { ProcessFoldHeader } from "./ProcessFoldHeader";
import { CompactionCard, NoticeCard, PhaseCard, SteerCard } from "./TranscriptCards";
import { LiveStreamContext } from "./LiveStreamContext";
import { useTranscriptSelectableRows } from "../lib/useTranscriptSelectableRows";
import { useCreationTranscriptScrollbar } from "../lib/useCreationTranscriptScrollbar";
import { useTranscriptScrollInteractions } from "../lib/useTranscriptScrollInteractions";
import { hasTranscriptScrollableRange, TRANSCRIPT_AT_BOTTOM_THRESHOLD_PX, useTranscriptScrollArbiter } from "../lib/useTranscriptScrollArbiter";
import { useTranscriptLayoutIntegrity } from "../lib/useTranscriptLayoutIntegrity";
import { TranscriptLayoutIntentProvider, TranscriptScrollWriteProvider } from "./TranscriptLayoutIntentContext";
import { MarkdownImageTabContext } from "./MarkdownImageContext";
import { recordTranscriptScrollDiagnostic } from "../lib/transcriptScrollProbe";
import { recordFrontendDiagnostic } from "../lib/frontendDiagnosticBridge";
import { useTranscriptQuestionJump, useTranscriptQuestions } from "../lib/useTranscriptQuestionNavigation";
import { useTranscriptHistoryAutoFill, useTranscriptPagingAuthorization, useTranscriptSurfaceCommit } from "../lib/useTranscriptNavigationSurface";
import { useTranscriptGeometryLifecycle } from "../lib/useTranscriptGeometryLifecycle";
import {
  LiveAssistantMessage,
  SHOW_SCROLL_DIAGNOSTICS,
  TRANSCRIPT_VIRTUOSO_COMPONENTS,
  TRANSCRIPT_VIRTUOSO_COMPONENTS_WITH_HEADER,
  type TranscriptVirtuosoContext,
} from "./TranscriptVirtuosoParts";

// NoticeCard lives with the other row cards; keep the historical export path.
export { NoticeCard } from "./TranscriptCards";
type OpenTurnAction = { turn: number; menu: "summary" | "rewind" };
const QUESTION_NAV_MIN_COUNT = 2;
const EMPTY_CHECKPOINTS: CheckpointMeta[] = [];
const EMPTY_INVOCATION_METADATA: InvocationMetadataMap = {};
const NO_HELD_ROWS: readonly TranscriptRow[] = [];
const QuestionJumpBar = lazy(() => import("./QuestionJumpBar"));
const SHOW_FRONTEND_DIAGNOSTICS = typeof __BUILD_CHANNEL__ === "undefined"
  || __BUILD_CHANNEL__ === "test"
  || __BUILD_CHANNEL__ === "preview"
  || __BUILD_CHANNEL__ === "canary"
  || Boolean(import.meta.env?.DEV);
const FrontendDiagnosticsPanel = SHOW_FRONTEND_DIAGNOSTICS
  ? lazy(() => import("./FrontendDiagnosticsPanel"))
  : null;
const VIRTUAL_OVERSCAN_ROWS = 8;
const READER_MOUNT_CORRIDOR_ROWS = 112;
const READER_MOUNT_CORRIDOR_VIEWPORTS = 7;
// Keep paged history measured during manual reading so WKWebView cannot replace
// non-overlapping ranges without an anchor; large sessions keep a bounded corridor.
const READER_FULL_MOUNT_ROW_LIMIT = 1_000;

export function Transcript({
  items,
  live: liveProp,
  liveStore,
  tabId,
  geometrySessionKey,
  footerHeight = 0,
  onPrompt,
  onDeliveryContinue,
  onAcceptDelivery,
  onOpenChanges,
  onOpenVerification,
  onEditPrompt,
  onRewind,
  checkpoints = EMPTY_CHECKPOINTS,
  actionPending = false,
  rewindDisabled = false,
  running = false,
  questionNavigator = true,
  welcomeVariant = "default",
  creationMode = false,
  actionHoverMenus = false,
  rewindSignal = 0,
  revealSignal = 0,
  hydrating = false,
  hasOlderHistory = false,
  historyStartTurn = 0,
  historyTotalTurns = 0,
  loadingOlderHistory = false,
  olderHistoryError,
  onLoadOlderHistory,
  turnStartAt,
  contentRevision = 0,
  invocationMetadata = EMPTY_INVOCATION_METADATA,
  historyMutation,
  surfaceCommitToken,
  onSurfacePaintReady,
}: {
  items: Item[];
  live?: LiveStream;
  liveStore?: ControllerLiveStore;
  tabId?: string;
  geometrySessionKey?: string;
  footerHeight?: number;
  onPrompt: (text: string) => void;
  onDeliveryContinue?: () => void;
  onAcceptDelivery?: () => void;
  onOpenChanges?: () => void;
  onOpenVerification?: (summary: WireCompletionSummary) => void;
  onEditPrompt?: (turn: number, displayText: string, submitText?: string) => boolean | void | Promise<boolean | void>;
  onRewind?: (turn: number, scope: string) => void;
  checkpoints?: CheckpointMeta[];
  actionPending?: boolean;
  rewindDisabled?: boolean;
  running?: boolean;
  questionNavigator?: boolean;
  welcomeVariant?: "default" | "creation";
  creationMode?: boolean;
  actionHoverMenus?: boolean;
  rewindSignal?: number;
  revealSignal?: number;
  hydrating?: boolean;
  hasOlderHistory?: boolean;
  historyStartTurn?: number;
  historyTotalTurns?: number;
  loadingOlderHistory?: boolean;
  olderHistoryError?: string;
  onLoadOlderHistory?: (targetTurn?: number, trigger?: HistoryLoadTrigger) => boolean | Promise<boolean>;
  turnStartAt?: number;
  contentRevision?: number;
  invocationMetadata?: InvocationMetadataMap;
  historyMutation?: HistoryMutation;
  surfaceCommitToken?: string;
  onSurfacePaintReady?: (token: string, outcome: "ready" | "degraded") => void;
}) {
  const t = useT();
  const subscribeLive = useCallback(
    (listener: () => void) => liveStore?.subscribe(tabId, listener) ?? (() => {}),
    [liveStore, tabId],
  );
  const getLiveSnapshot = useCallback(
    () => liveStore?.getSnapshot(tabId) ?? liveProp,
    [liveProp, liveStore, tabId],
  );
  const live = useSyncExternalStore(subscribeLive, getLiveSnapshot, getLiveSnapshot);
  const layoutSurfaceKey = `${tabId ?? ""}:${revealSignal}`;
  const resolvedGeometrySessionKey = geometrySessionKey || `tab:${tabId ?? "preview"}`;
  // Transcript survives tab switches; the bounded LRU therefore survives
  // reveal resets while the Virtuoso view state still resets independently.
  const measuredSizes = useMemo(() => createTranscriptMeasuredSizes(), []);
  const [geometryEnvironment, setGeometryEnvironment] = useState<TranscriptGeometryEnvironment>({
    contentWidth: undefined,
    typographySignature: "unresolved",
  });
  const geometrySessionKeyRef = useRef(resolvedGeometrySessionKey);
  geometrySessionKeyRef.current = resolvedGeometrySessionKey;
  const geometryEnvironmentRef = useRef(geometryEnvironment);
  geometryEnvironmentRef.current = geometryEnvironment;
  const recordMeasuredGeometry = useCallback((
    rowKey: string,
    kind: TranscriptRow["kind"],
    layoutVariant: TranscriptRowLayoutVariant,
    height: number,
    width: number,
    measurementVersion: string | undefined,
    _estimateSource: TranscriptEstimateSource | undefined,
    staticEstimate: number | undefined,
  ) => {
    measuredSizes.recordGeometry(geometrySessionKeyRef.current, {
      rowKey,
      kind,
      layoutVariant,
      height,
      environment: { ...geometryEnvironmentRef.current, contentWidth: width },
      measurementVersion: measurementVersion ?? "0:0",
      staticEstimate,
    });
  }, [measuredSizes]);
  useEffect(() => {
    recordFrontendDiagnostic("transcript", "transcript.surface", {
      hasActiveTab: Boolean(tabId),
      totalRows: items.length,
    });
  }, [items.length, layoutSurfaceKey, tabId]);
  const [layoutWidth, setLayoutWidth] = useState<number>();
  const [geometryBootstrapComplete, setGeometryBootstrapComplete] = useState(false);
  const geometryBootstrapRevisionRef = useRef<string | null>(null);
  const {
    virtuosoRef,
    scrollRef,
    itemSize,
    nativeScrollbarDragging,
    readerTransactionActive,
    modeRef: scrollModeRef,
    scrollElement,
    pinnedRef: stick,
    onWheelIntent,
    onPointerDownIntent,
    onNestedScrollIntent,
    onTouchStartIntent,
    onTouchMoveIntent,
    onTouchEndIntent,
    onKeyScrollIntent,
    isAtBottom,
    scrollerRef,
    atBottomStateChange,
    deliverScroll,
    scrollToBottom,
    pinLiveTailBeforePaint,
    followGrowingTail,
    revalidateTail,
    beginUserResize, userResizeRevision,
    scrollToDataIndex, beginQuestionJump, finishQuestionJump,
    releaseTailFollow,
    setMode: setScrollMode,
    writeOffset,
    reset: resetScroll,
    finishProgrammaticScroll,
    submitRecoveryRequest,
    retryRecoveryRequest,
    lastGoodAnchorRef,
    layoutTransientRef,
  } = useTranscriptScrollArbiter({
    onRecoveryTerminal: noteTranscriptRecoveryTerminal,
    onItemMeasured: recordMeasuredGeometry,
  });
  const virtuosoReadyRef = useRef(false);
  const entranceRef = useTranscriptEntranceAnimation<HTMLDivElement>(tabId, revealSignal, items);

  // Lease the markdown parse worker for as long as a transcript surface is
  // mounted; the last release terminates the thread (it re-spawns lazily).
  useEffect(() => {
    acquireMarkdownWorkerClient();
    return () => releaseMarkdownWorkerClient();
  }, []);

  const cancelStreamingAutoScroll = useCallback(() => {}, []);

  const cancelStreamingAndFollow = useCallback(() => {
    cancelStreamingAutoScroll();
    releaseTailFollow();
  }, [cancelStreamingAutoScroll, releaseTailFollow]);

  const {
    state: creationScrollbar,
    handleScroll: handleCreationScroll,
    onThumbPointerDown: handleCreationScrollbarThumbPointerDown,
    onRailPointerDown: handleCreationScrollbarRailPointerDown,
  } = useCreationTranscriptScrollbar({
    enabled: creationMode,
    contentRevision: items.length,
    scrollRef,
    onScroll: () => {},
    setScrollMode,
    writeOffset,
    finishProgrammaticScroll,
  });

  const [
    questions,
    loadedByTurn,
    totalQuestions,
    activeQuestion,
    setActiveQuestion,
    scheduleActiveQuestionSync,
    turnForUser,
    lastTurn,
  ] = useTranscriptQuestions(items, historyStartTurn, historyTotalTurns, scrollElement, scrollToBottom);
  const showQuestionNav = questionNavigator && totalQuestions >= QUESTION_NAV_MIN_COUNT;

  // Transcript is not keyed by tab, so reset the previous tab's pin and open the new session at its tail (#4584).
  useLayoutEffect(() => {
    resetScroll();
    virtuosoReadyRef.current = false;
  }, [resetScroll, revealSignal, tabId]);

  useLayoutEffect(() => {
    if (!virtuosoReadyRef.current || !stick.current) return;
    scrollToBottom();
  }, [footerHeight, scrollToBottom, stick]);

  const refreshGeometryEnvironment = useCallback((element: HTMLElement) => {
    const next = readTranscriptGeometryEnvironment(element);
    setGeometryEnvironment((current) => (
      Math.abs((current.contentWidth ?? 0) - (next.contentWidth ?? 0)) <= 1
        && current.typographySignature === next.typographySignature
        ? current
        : next
    ));
  }, []);

  // The live region grows from zero height and shrinks the history viewport
  // mid-stream; keep the tail pinned across that viewport resize.
  useEffect(() => {
    const element = scrollElement;
    if (!element || typeof ResizeObserver === "undefined") return;
    let lastHeight = element.clientHeight;
    let lastWidth = element.clientWidth;
    setLayoutWidth(lastWidth);
    refreshGeometryEnvironment(element);
    const observer = new ResizeObserver(() => {
      const height = element.clientHeight;
      const width = element.clientWidth;
      if (width !== lastWidth) {
        lastWidth = width;
        setLayoutWidth(width);
        refreshGeometryEnvironment(element);
        if (!hydrating) followGrowingTail("typography-change");
      }
      if (height !== lastHeight) {
        lastHeight = height;
        if (!hydrating || scrollModeRef.current === "tail-follow") followGrowingTail("viewport-resize");
      }
    });
    observer.observe(element);
    return () => observer.disconnect();
  }, [hydrating, scrollElement, followGrowingTail, refreshGeometryEnvironment, scrollModeRef]);

  // Typography settings update CSS variables synchronously. Re-read the
  // geometry signature without remounting Virtuoso; old exact samples then
  // fail their font key and static state-aware seeds take over.
  useEffect(() => onTypographyPreferencesChange(() => {
    if (scrollElement) refreshGeometryEnvironment(scrollElement);
  }), [refreshGeometryEnvironment, scrollElement]);

  // Sub-agent calls carry a parentId; collect them under their parent `task`
  // call so the parent card can render them nested, and skip them at top level.
  const subcallsByParent = useMemo(() => {
    const m = new Map<string, ToolItem[]>();
    for (const it of items) {
      if (it.kind === "tool" && it.parentId) {
        const arr = m.get(it.parentId) ?? [];
        arr.push(it);
        m.set(it.parentId, arr);
      }
    }
    return m;
  }, [items]);

  // ── Turn models, fold state, virtual rows ─────────────────────────────────
  // The row model only depends on structural inputs and live PRESENCE flags —
  // streaming tokens flow through LiveStreamContext and never rebuild it.
  const liveId = live?.id;
  const liveHasAnswerText = Boolean(live?.text.trim());
  const liveHasReasoning = Boolean(live?.reasoning);
  const liveReasoningComplete = live?.reasoningComplete;
  const reasoningDisplayMode = useReasoningDisplayMode();
  const hideReasoning = reasoningDisplayMode === "hidden" || reasoningDisplayMode === "pending";
  const liveFlags = useMemo<TranscriptLiveFlags>(
    () => (liveId
      ? { id: liveId, hasAnswerText: liveHasAnswerText, hasReasoning: liveHasReasoning, reasoningComplete: liveReasoningComplete }
      : NO_LIVE),
    [liveId, liveHasAnswerText, liveHasReasoning, liveReasoningComplete],
  );
  const turnModels = useMemo(() => buildTurnModels(items, liveFlags, running, hideReasoning), [items, liveFlags, running, hideReasoning]);
  const segmentStates = useMemo(() => foldSegmentStates(turnModels, reasoningDisplayMode === "expanded"), [reasoningDisplayMode, turnModels]);

  const [foldPreference, setFoldPreference] = useState<ProcessFoldPreference>(getProcessFoldPreference);
  useEffect(() => onProcessFoldPreferenceChange(setFoldPreference), []);
  const foldPreferenceRef = useRef(foldPreference);
  const [folds, setFolds] = useState<FoldMap>(EMPTY_FOLDS);

  // Hoisted TurnCollapse effects: auto-open while running, auto-close on
  // completion, preference switches apply to folds already on screen.
  useEffect(() => {
    const preferenceChanged = foldPreferenceRef.current !== foldPreference;
    foldPreferenceRef.current = foldPreference;
    setFolds((prev) => reconcileFoldEntries(prev, segmentStates, foldPreference, preferenceChanged) ?? prev);
  }, [segmentStates, foldPreference]);

  const handleFoldToggle = useCallback((segmentKey: string, currentlyOpen: boolean) => {
    beginUserResize();
    setFolds((prev) => foldMapWithToggle(prev, segmentKey, currentlyOpen));
  }, [beginUserResize]);

  const handleReasoningManualOpen = useCallback((segmentKey: string) => {
    beginUserResize();
    const running = segmentStates.find((segment) => segment.key === segmentKey)?.hasRunningWork ?? false;
    setFolds((prev) => foldMapWithReasoningOpen(prev, segmentKey, running));
  }, [beginUserResize, segmentStates]);

  // ── The turn action menu ──────────────────────────────────────────────────
  const [openAction, setOpenAction] = useState<OpenTurnAction | null>(null);
  useEffect(() => {
    if (openAction === null) return;
    const onDown = (e: MouseEvent) => {
      const el = e.target as Element | null;
      if (!el || !el.closest(".turn-actions")) setOpenAction(null);
    };
    document.addEventListener("mousedown", onDown);
    return () => document.removeEventListener("mousedown", onDown);
  }, [openAction]);

  const checkpointsByTurn = useMemo(() => new Map(checkpoints.map((checkpoint) => [checkpoint.turn, checkpoint])), [checkpoints]);
  const hasCheckpointForTurn = useCallback((turn: number) => checkpointsByTurn.has(turn), [checkpointsByTurn]);
  const rows = useMemo(
    () => buildTranscriptRows(turnModels, {
      folds,
      foldPreference,
      hasOlderHistory,
      creationMode,
      turnForUser,
      hasCheckpointForTurn,
      reasoningDisplayMode,
      subcallsByParent,
    }),
    [turnModels, folds, foldPreference, hasOlderHistory, creationMode, turnForUser, hasCheckpointForTurn, reasoningDisplayMode, subcallsByParent],
  );
  const { liveSplit, liveMinHeight } = useTranscriptLiveTurnStability({
    turnModels, rows, liveId, running, stabilityKey: `${layoutSurfaceKey}:${userResizeRevision}`,
    scrollElement, hydrating, tailOwnedRef: stick, pinLiveTailBeforePaint,
  });
  // Keep the load-older affordance in Virtuoso's measured Header slot so an
  // older page is a true data prepend, rather than an insertion after row 0.
  const virtualRows = useMemo(
    () => liveSplit.historyRows[0]?.kind === "older-history" ? liveSplit.historyRows.slice(1) : liveSplit.historyRows,
    [liveSplit.historyRows],
  );
  const rowIndexByKey = useMemo(() => {
    const map = new Map<string, number>();
    virtualRows.forEach((row, index) => map.set(String(row.key), index));
    return map;
  }, [virtualRows]);
  // Selection spans both regions: the logical model covers history + live
  // rows, while Virtuoso index jumps keep using the history-only map above.
  const allRows = useMemo(
    () => [...virtualRows, ...liveSplit.liveRows],
    [virtualRows, liveSplit.liveRows],
  );
  const allRowIndexByKey = useMemo(() => {
    const map = new Map<string, number>();
    allRows.forEach((row, index) => map.set(String(row.key), index));
    return map;
  }, [allRows]);
  const [selectableRows, liveSelectableRows] = useTranscriptSelectableRows(allRows, live);
  const {
    resetKey: virtuosoResetKey,
    firstItemIndex,
    restoreLocation,
    restoreSnapshot,
    handleItemsRendered: handleRecoveryItemsRendered,
    scheduleBlankViewportCheck,
    invalidateAnchors,
    noteUserScrollIntent,
    noteScrollActivity,
    safeMode: layoutSafeMode,
  } = useTranscriptLayoutIntegrity({
    surfaceKey: layoutSurfaceKey,
    rows: virtualRows,
    rowIndexByKey,
    scrollRef,
    pinnedRef: stick,
    readyRef: virtuosoReadyRef,
    scrollToBottom,
    submitRecoveryRequest,
    retryRecoveryRequest,
    lastGoodAnchorRef,
    layoutTransientRef,
    layoutWidth,
    geometrySessionKey: resolvedGeometrySessionKey,
    geometryEnvironment,
  });
  const selectionRetention = useTranscriptSelectionRetention({
    tabId,
    revealSignal,
    rowIndexByKey: allRowIndexByKey,
    selectableRows,
    selectableRowOverrides: liveSelectableRows,
    scrollRef,
    setScrollMode,
    writeOffset,
    cancelStreamingScroll: cancelStreamingAndFollow,
  });
  const clearTranscriptSelection = selectionRetention.clear;
  const { readySurfaceKey: surfacePaintReadySurfaceKey, markItemsRendered: markSurfaceItemsRendered } = useTranscriptSurfaceCommit({
    token: surfaceCommitToken, hydrating, layoutSurfaceKey, virtualRowCount: virtualRows.length, scrollRef, virtuosoReadyRef,
    layoutTransientRef, scheduleRecovery: scheduleBlankViewportCheck, onReady: onSurfacePaintReady,
  });
  const pagingAuthorization = useTranscriptPagingAuthorization({
    layoutSurfaceKey, nativeScrollbarDragging, scrollRef, noteUserScrollIntent, onWheelIntent, onWheelAccepted: SHOW_SCROLL_DIAGNOSTICS ? (deltaY) => recordTranscriptScrollDiagnostic("wheel", { deltaY }) : undefined,
    onTouchStartIntent, onTouchMoveIntent, onKeyScrollIntent, onPointerDownIntent,
  });
  const scrollInteractions = useTranscriptScrollInteractions({
    scrollElement,
    cancelStreamingScroll: cancelStreamingAutoScroll,
    onWheelIntent: pagingAuthorization.onWheelIntent,
    onTouchMoveIntent: pagingAuthorization.onTouchMoveIntent,
    onTouchEndIntent,
    onKeyScrollIntent: pagingAuthorization.onKeyScrollIntent,
    onPointerDownIntent: pagingAuthorization.onPointerDownIntent,
    onNestedScrollIntent,
    onScrollEnd: finishProgrammaticScroll,
    onSelectionPointerDown: selectionRetention.onPointerDownCapture,
  });
  const virtualRowsGeometryRevision = useMemo(
    () => virtualRows.map((row) => [
      String(row.key),
      row.kind,
      transcriptRowLayoutVariant(row),
      transcriptRowMeasurementVersion(row),
    ].join("\u0000")).join("\u0001"),
    [virtualRows],
  );
  const geometryEnvironmentReady = geometryEnvironment.typographySignature !== "unresolved"
    && Number.isFinite(geometryEnvironment.contentWidth)
    && (geometryEnvironment.contentWidth ?? 0) > 0;
  // Fold reconciliation and initial history normalization run in effects. Do
  // not let Virtuoso construct its first size tree between those two commits:
  // that would seed one tree from the pre-fold row model and then apply a
  // whole-list geometry delta during the user's first upward gesture. Wait
  // for one quiet animation frame after the environment and row revision are
  // both stable; subsequent revisions remain incremental and do not remount.
  useEffect(() => {
    if (geometryBootstrapComplete || !geometryEnvironmentReady) return;
    const revision = virtualRowsGeometryRevision;
    geometryBootstrapRevisionRef.current = revision;
    let frame = requestAnimationFrame(() => {
      frame = requestAnimationFrame(() => {
        if (geometryBootstrapRevisionRef.current === revision) setGeometryBootstrapComplete(true);
      });
    });
    return () => cancelAnimationFrame(frame);
  }, [geometryBootstrapComplete, geometryEnvironmentReady, virtualRowsGeometryRevision]);
  // Measurements mutate the bounded cache but must never rebuild the whole
  // array while a Virtuoso surface is alive. A ref-backed seed makes this
  // explicit: only a real geometry-contract key (row state/content, width or
  // typography, session, or an explicit reset) synthesizes a new initial
  // seed. This prevents calibration samples arriving during the first upward
  // traversal from changing the size tree underneath the pointer.
  const geometrySeedKey = [
    resolvedGeometrySessionKey,
    virtuosoResetKey,
    geometryEnvironment.contentWidth ?? "unknown",
    geometryEnvironment.typographySignature,
    virtualRowsGeometryRevision,
  ].join("\u0002");
  const geometrySeedRef = useRef<{ key: string; value: TranscriptSynthesizedSizes } | null>(null);
  if (geometrySeedRef.current?.key !== geometrySeedKey) {
    geometrySeedRef.current = {
      key: geometrySeedKey,
      value: measuredSizes.synthesizeDetailed(resolvedGeometrySessionKey, virtualRows, geometryEnvironment),
    };
  }
  const synthesizedSizes = geometrySeedRef.current.value;
  const heightEstimates = synthesizedSizes.heightEstimates;
  const estimateSources = synthesizedSizes.estimateSources;
  const estimatedTotalHeight = useMemo(
    () => heightEstimates.reduce((total, height) => total + height, 0),
    [heightEstimates],
  );
  const overlayRevision = useMemo(
    () => virtualRows.map((row) => String(row.key)).join("|"),
    [virtualRows],
  );
  const handleScrollerRef = useCallback((node: HTMLElement | Window | null) => {
    scrollerRef(node);
    const element = node instanceof HTMLElement ? node as HTMLDivElement : null;
    entranceRef.current = element;
    if (element) {
      setLayoutWidth(element.clientWidth);
      refreshGeometryEnvironment(element);
    }
  }, [entranceRef, refreshGeometryEnvironment, scrollerRef]);
  const handleTranscriptScroll = useCallback(() => {
    deliverScroll();
    noteScrollActivity();
    pagingAuthorization.noteScrollPosition();
    if (creationMode) handleCreationScroll();
    scheduleActiveQuestionSync();
    scheduleBlankViewportCheck();
  }, [creationMode, deliverScroll, handleCreationScroll, noteScrollActivity, pagingAuthorization, scheduleActiveQuestionSync, scheduleBlankViewportCheck]);
  const [handleJumpToQuestion, handleEarlierHistoryReached, retryOlderHistory, questionJumpSurface] = useTranscriptQuestionJump({
    questions, loadedByTurn, layoutSurfaceKey, rowIndexByKey,
    hasOlderHistory, loadingOlderHistory, olderHistoryError, running, scrollElement, scheduleRecovery: scheduleBlankViewportCheck,
    onLoadOlderHistory, clearTranscriptSelection, invalidateAnchors,
    beginQuestionJump, finishQuestionJump, scrollToDataIndex, setActiveQuestion, rewindSignal,
  });
  const handleViewportEarlierHistoryReached = useCallback(() => {
    if (hydrating || !pagingAuthorization.consume()) return;
    void Promise.resolve(handleEarlierHistoryReached()).finally(pagingAuthorization.complete);
  }, [handleEarlierHistoryReached, hydrating, pagingAuthorization]);
  useTranscriptHistoryAutoFill({
    readySurfaceKey: surfacePaintReadySurfaceKey, layoutSurfaceKey, hydrating, hasOlderHistory, loadingOlderHistory,
    olderHistoryError, running, historyStartTurn, virtualRowCount: virtualRows.length, scrollRef, virtuosoReadyRef,
    layoutTransientRef, onLoadOlderHistory,
  });

  // The jump-bottom click is explicit user intent: it outranks any in-flight
  // recovery anchor restore and ends a stale selection gesture whose
  // pointerup was lost (#8657/#8688).
  const handleJumpToBottom = () => {
    selectionRetention.endStaleGesture();
    invalidateAnchors();
    scrollToBottom();
  };

  const empty = items.length === 0;
  const geometryReady = geometryEnvironmentReady && geometryBootstrapComplete;

  // ── Row rendering ─────────────────────────────────────────────────────────
  // renderRow/itemContent keep stable identities: Transcript re-renders on
  // every streaming frame, and Virtuoso re-maps every mounted row whenever
  // itemContent changes identity.
  const renderRow = useCallback((row: TranscriptRow): ReactNode => {
    switch (row.kind) {
      case "older-history":
        return null;
      case "user": {
        const user = row.item;
        const checkpoint = row.turn == null ? undefined : checkpointsByTurn.get(row.turn);
        return (
          <UserMessage
            id={user.id}
            text={user.text}
            submitText={user.submitText}
            failed={user.failed}
            createdAt={user.createdAt}
            turn={row.turn}
            anchorId={questionAnchorId(user.id)}
            onEdit={onEditPrompt}
            editDisabled={rewindDisabled || !checkpoint?.canConversation}
          />
        );
      }
      case "process-header":
        return (
          <ProcessFoldHeader
            segment={row.segment}
            open={row.open}
            onToggle={() => handleFoldToggle(row.segment.key, row.open)}
            turnStartAt={row.segment.turnActive ? turnStartAt : undefined}
          />
        );
      case "reasoning":
        return (
          <div className="turn-collapse__body">
            <InlineAssistantReasoning
              item={row.item}
              autoFollowActive={row.autoFollowActive}
              onManualOpen={() => handleReasoningManualOpen(row.segmentKey)}
            />
          </div>
        );
      case "tool":
        return (
          <div className="turn-collapse__body">
            <ToolCard item={row.item} subcalls={subcallsByParent.get(row.item.id)} tabId={tabId} />
          </div>
        );
      case "tool-batch":
        return (
          <div className="turn-collapse__body">
            <ReadOnlyBatch items={row.items} subcalls={subcallsByParent} tabId={tabId} />
          </div>
        );
      case "tool-group":
        return (
          <div className="turn-collapse__body">
            <ToolGroup kind={row.groupKind} items={row.items} subcalls={subcallsByParent} tabId={tabId} />
          </div>
        );
      case "phase":
        return (
          <div className="turn-collapse__body">
            <PhaseCard id={row.item.id} text={row.item.text} />
          </div>
        );
      case "process-notice":
        return (
          <div className="turn-collapse__body">
            <NoticeCard item={row.item} />
          </div>
        );
      case "compaction":
        return (
          <div className="turn-collapse__body">
            <CompactionCard item={row.item} />
          </div>
        );
      case "answer":
        return (
          <LiveAssistantMessage
            item={assistantAnswerOnly(row.item)}
            defaultExpanded={false}
            expandWhileStreaming={false}
            creationMode={creationMode}
            reasoningDisplay="hide"
          />
        );
      case "notice":
        if (isSteerNoticeText(row.item.text)) {
          return <SteerCard id={row.item.id} text={row.item.text} />;
        }
        return (
          <NoticeCard
            item={row.item}
            actionDisabled={running}
            onAction={row.item.action === "continue_delivery"
              ? (onDeliveryContinue ?? (() => onPrompt(t("notice.deliveryIncompleteContinuePrompt"))))
              : row.item.action === "open_changes"
                ? onOpenChanges
                : undefined}
            onOpenVerification={row.item.variant === "completion" ? onOpenVerification : undefined}
            onAccept={row.item.action === "continue_delivery" ? onAcceptDelivery : undefined}
          />
        );
      case "extension":
        return <ExtensionCard item={row.item} tabId={tabId} />;
      case "turn-actions": {
        const openMenu = openAction && openAction.turn === row.turn ? openAction.menu : null;
        return (
          <TurnActions
            text={row.text}
            turn={row.turn}
            openMenu={openMenu}
            onOpenMenu={(menu) => setOpenAction(menu ? { turn: row.turn, menu } : null)}
            checkpoint={checkpointsByTurn.get(row.turn)}
            actionPending={actionPending}
            rewindDisabled={rewindDisabled}
            hoverMenus={actionHoverMenus}
            isLastTurn={row.turn === lastTurn}
            onRewind={(targetTurn, scope) => {
              onRewind?.(targetTurn, scope);
              setOpenAction(null);
            }}
          />
        );
      }
    }
  }, [
    actionHoverMenus,
    actionPending,
    checkpointsByTurn,
    creationMode,
    handleFoldToggle,
    handleReasoningManualOpen,
    lastTurn,
    onDeliveryContinue,
    onAcceptDelivery,
    onEditPrompt,
    onOpenChanges,
    onOpenVerification,
    onPrompt,
    onRewind,
    openAction,
    rewindDisabled,
    running,
    subcallsByParent,
    t,
    tabId,
    turnStartAt,
  ]);
  const renderVirtuosoRow = useCallback(
    (_index: number, row: TranscriptRow) => renderRow(row),
    [renderRow],
  );

  // ── Live-region completion handoff ────────────────────────────────────────
  // When the active turn settles, its rows join the virtual data in the same
  // commit that would unmount the live-region footer. While the view is at
  // the bottom, keep painting the region's final content until Virtuoso
  // reports the materialized tail row mounted, so completion does not flash
  // stale history (#8657/#8688).
  const heldLiveRowsRef = useRef<readonly TranscriptRow[]>([]);
  const heldSurfaceRef = useRef(layoutSurfaceKey);
  const [holdingLiveRegion, setHoldingLiveRegion] = useState(false);
  const wasLiveActiveRef = useRef(false);
  if (liveSplit.liveActive) {
    wasLiveActiveRef.current = true;
    heldSurfaceRef.current = layoutSurfaceKey;
    heldLiveRowsRef.current = liveSplit.liveRows;
    if (holdingLiveRegion) setHoldingLiveRegion(false);
  } else if (wasLiveActiveRef.current) {
    wasLiveActiveRef.current = false;
    // Transcript is not keyed by tab: a hold captured on one surface must
    // never paint into another after a tab switch.
    if (heldSurfaceRef.current !== layoutSurfaceKey) heldLiveRowsRef.current = [];
    if (heldLiveRowsRef.current.length > 0 && isAtBottom && !holdingLiveRegion) {
      setHoldingLiveRegion(true);
    }
  }
  // The materialization target can disappear mid-hold (rewind, fork, or a
  // wholesale session replace): release immediately instead of pinning rows
  // that are no longer in the transcript for the safety-timeout duration.
  if (holdingLiveRegion && heldLiveRowsRef.current.length > 0) {
    const lastHeldKey = String(heldLiveRowsRef.current[heldLiveRowsRef.current.length - 1].key);
    if (!rows.some((row) => String(row.key) === lastHeldKey)) {
      heldLiveRowsRef.current = [];
      setHoldingLiveRegion(false);
    }
  }
  useEffect(() => {
    heldLiveRowsRef.current = [];
    setHoldingLiveRegion(false);
  }, [layoutSurfaceKey]);
  useEffect(() => {
    if (!holdingLiveRegion) return;
    // Safety net: if the tail row never reports (e.g. the surface changed),
    // release the hold instead of pinning stale content.
    const timeout = window.setTimeout(() => {
      heldLiveRowsRef.current = [];
      setHoldingLiveRegion(false);
    }, 300);
    return () => window.clearTimeout(timeout);
  }, [holdingLiveRegion]);
  useLayoutEffect(() => {
    if (!holdingLiveRegion || !scrollRef.current) return;
    const held = heldLiveRowsRef.current;
    const lastKey = held.length > 0 ? String(held[held.length - 1].key) : null;
    if (lastKey === null) return;
    const mounted = Array.from(scrollRef.current.querySelectorAll<HTMLElement>(".transcript__row[data-row-key]"))
      .some((row) => row.dataset.rowKey === lastKey && !row.closest('[data-live-region="true"]'));
    if (!mounted) return;
    heldLiveRowsRef.current = [];
    setHoldingLiveRegion(false);
  }, [contentRevision, holdingLiveRegion, scrollRef, virtualRows.length]);
  const heldLiveRows = heldSurfaceRef.current === layoutSurfaceKey ? heldLiveRowsRef.current : NO_HELD_ROWS;
  const showLiveRegion = liveSplit.liveActive || (holdingLiveRegion && heldLiveRows.length > 0);
  const readerMountCorridorRows = readerTransactionActive && virtualRows.length <= READER_FULL_MOUNT_ROW_LIMIT
    ? Math.max(READER_MOUNT_CORRIDOR_ROWS, virtualRows.length)
    : READER_MOUNT_CORRIDOR_ROWS;

  const { handleItemsRendered, handleTotalListHeightChanged } = useTranscriptGeometryLifecycle({
    virtualRowCount: virtualRows.length, hydrating, readerTransactionActive, historyMutation, scrollModeRef,
    followGrowingTail, revalidateTail,
    reconcileLogicalFocus: selectionRetention.reconcileLogicalFocus,
    handleRecoveryItemsRendered, scheduleActiveQuestionSync, markSurfaceItemsRendered,
  });

  const virtuosoContext = useMemo<TranscriptVirtuosoContext>(() => ({
    tabId,
    scrollElement,
    nativeScrollbarDragging,
    overlayRevision,
    geometryEnvironment,
    rowGeometry: {
      heightEstimates,
      estimateSources,
      rowIndexByKey,
      contentRevision,
    },
    liveRegion: showLiveRegion
      ? {
          rows: liveSplit.liveActive ? liveSplit.liveRows : heldLiveRows,
          renderRow,
          showStatus: liveSplit.liveActive,
          overlay: !liveSplit.liveActive,
          turnStartAt,
          minHeight: liveMinHeight ?? undefined,
          onPointerDownCapture: selectionRetention.onPointerDownCapture,
        }
      : null,
    olderHistory: hasOlderHistory && (loadingOlderHistory || Boolean(olderHistoryError))
      ? {
          loading: loadingOlderHistory,
          error: olderHistoryError ? t("transcript.loadEarlierFailed") : undefined,
          onRetry: retryOlderHistory,
        }
      : null,
  }), [
    hasOlderHistory,
    heightEstimates,
    estimateSources,
    geometryEnvironment,
    heldLiveRows,
    liveSplit.liveActive,
    liveSplit.liveRows,
    liveMinHeight,
    loadingOlderHistory,
    contentRevision,
    nativeScrollbarDragging,
    olderHistoryError,
    overlayRevision,
    renderRow,
    rowIndexByKey,
    retryOlderHistory,
    scrollElement,
    selectionRetention.onPointerDownCapture,
    showLiveRegion,
    t,
    tabId,
    turnStartAt,
  ]);

  // ── Assemble rendered output ──────────────────────────────────────────────
  return (
    <InvocationMetadataContext.Provider value={invocationMetadata}>
    <MarkdownImageTabContext.Provider value={tabId ?? ""}>
    <TranscriptLayoutIntentProvider value={beginUserResize}>
    <TranscriptScrollWriteProvider value={writeOffset}>
    <div className="transcript-shell" aria-busy={Boolean(questionJumpSurface) || undefined}>
      {empty ? (
        <div
          className={`transcript transcript--empty${creationMode ? " transcript--creation-scrollbar" : ""}`}
          ref={(node) => handleScrollerRef(node)}
          aria-busy={hydrating || undefined}
        >
          {hydrating ? (
            <div className="transcript__loading" role="status" aria-live="polite">
              <Loader2 className="transcript__loading-icon" aria-hidden="true" />
              <span>{t("common.loading")}</span>
            </div>
          ) : <Welcome onPrompt={onPrompt} variant={welcomeVariant} />}
        </div>
      ) : !geometryReady ? (
        // Bootstrap the real readable width/font signature before Virtuoso
        // constructs its empty size tree. This element is the first scroller,
        // not a remount/recovery cycle; later environment changes stay live.
        <div
          className={`transcript${creationMode ? " transcript--creation-scrollbar" : ""}`}
          ref={(node) => handleScrollerRef(node)}
          aria-busy="true"
          data-transcript-geometry-bootstrap="true"
        />
      ) : (
        <LiveStreamContext.Provider value={live}>
          <Virtuoso<TranscriptRow, TranscriptVirtuosoContext>
            key={virtuosoResetKey}
            ref={virtuosoRef}
            className={`transcript${creationMode ? " transcript--creation-scrollbar" : ""}${creationMode && creationScrollbar.hot ? " transcript--scrollbar-hot" : ""}`}
            data-transcript-hydrating={hydrating ? "true" : "false"}
            data-transcript-reader-layout-lease={readerTransactionActive ? "true" : "false"}
            data-transcript-row-count={virtualRows.length}
            data-transcript-estimated-total={estimatedTotalHeight}
            data-transcript-reset-key={virtuosoResetKey}
            data-transcript-typography={geometryEnvironment.typographySignature}
            data-transcript-estimate-sources={JSON.stringify(estimateSources.reduce((counts, source) => {
              counts[source] = (counts[source] ?? 0) + 1;
              return counts;
            }, {} as Record<string, number>))}
            data={virtualRows}
            context={virtuosoContext}
            components={virtuosoContext.olderHistory ? TRANSCRIPT_VIRTUOSO_COMPONENTS_WITH_HEADER : TRANSCRIPT_VIRTUOSO_COMPONENTS}
            computeItemKey={(_index, row) => `${resolvedGeometrySessionKey}:${String(row.key)}`}
            firstItemIndex={firstItemIndex}
            // A validated captured state restores through the same stream as
            // initialTopMostItemIndex, so the two never apply together.
            restoreStateFrom={restoreSnapshot}
            initialTopMostItemIndex={restoreSnapshot ? undefined : restoreLocation}
            // Do not set alignToBottom: Virtuoso's margin-top:auto plus
            // firstItemIndex paints a ghost first-user bubble and empty band
            // in short chats. The coordinator owns tail following.
            atBottomThreshold={TRANSCRIPT_AT_BOTTOM_THRESHOLD_PX}
            atBottomStateChange={atBottomStateChange}
            heightEstimates={heightEstimates}
            itemSize={itemSize}
            minOverscanItemCount={layoutSafeMode || readerTransactionActive
              ? { top: readerMountCorridorRows, bottom: readerMountCorridorRows }
              : { top: VIRTUAL_OVERSCAN_ROWS, bottom: VIRTUAL_OVERSCAN_ROWS }}
            increaseViewportBy={layoutSafeMode || readerTransactionActive
              ? {
                  top: (scrollElement?.clientHeight ?? 0) * READER_MOUNT_CORRIDOR_VIEWPORTS,
                  bottom: (scrollElement?.clientHeight ?? 0) * READER_MOUNT_CORRIDOR_VIEWPORTS,
                }
              : { top: 480, bottom: 480 }}
            scrollerRef={handleScrollerRef}
            itemsRendered={handleItemsRendered}
            startReached={handleViewportEarlierHistoryReached}
            totalListHeightChanged={handleTotalListHeightChanged}
            itemContent={renderVirtuosoRow}
            onScroll={handleTranscriptScroll}
            onWheelCapture={scrollInteractions.onWheelCapture}
            onTouchStartCapture={pagingAuthorization.onTouchStartIntent}
            onTouchMoveCapture={scrollInteractions.onTouchMoveCapture}
            onTouchEndCapture={scrollInteractions.onTouchEndCapture}
            onTouchCancelCapture={scrollInteractions.onTouchEndCapture}
            onKeyDownCapture={scrollInteractions.onKeyDownCapture}
            onPointerDownCapture={scrollInteractions.onPointerDownCapture}
          />
        </LiveStreamContext.Provider>
      )}

      {creationMode && creationScrollbar.visible && (
        <div
          className={`transcript__scrollbar${creationScrollbar.hot ? " transcript__scrollbar--hot" : ""}`}
          onPointerDown={handleCreationScrollbarRailPointerDown}
          aria-hidden="true"
        >
          <div
            className="transcript__scrollbar-thumb"
            style={{ top: creationScrollbar.thumbTop, height: creationScrollbar.thumbHeight } as CSSProperties}
            onPointerDown={handleCreationScrollbarThumbPointerDown}
          />
        </div>
      )}

      {!empty && showQuestionNav && (
        <Suspense fallback={null}>
          <QuestionJumpBar
            loadedQuestions={questions}
            totalQuestions={totalQuestions}
            activeTurn={activeQuestion}
            onJump={handleJumpToQuestion}
          />
        </Suspense>
      )}

      {!empty && !isAtBottom && scrollElement && hasTranscriptScrollableRange(scrollElement) && (
        <button
          type="button"
          className="transcript__jump-bottom"
          onClick={handleJumpToBottom}
          aria-label={t("transcript.jumpToBottom")}
          title={t("transcript.jumpToBottom")}
        >
          <ArrowDown size={18} strokeWidth={2.2} aria-hidden="true" />
        </button>
      )}
      {questionJumpSurface && (
        <div
          className="transcript-navigation-overlay transcript-question-jump-overlay"
          data-question-jump-mask="true" data-question-jump-phase={questionJumpSurface.phase}
          role="status" aria-live="polite"
        >
          <span className="transcript-navigation-overlay__spinner" aria-hidden="true" />
          <span>{t("common.loading")}</span>
        </div>
      )}
      {FrontendDiagnosticsPanel && <Suspense fallback={null}><FrontendDiagnosticsPanel scrollElement={scrollElement} totalRows={virtualRows.length} /></Suspense>}
    </div>
    </TranscriptScrollWriteProvider>
    </TranscriptLayoutIntentProvider>
    </MarkdownImageTabContext.Provider>
    </InvocationMetadataContext.Provider>
  );
}
