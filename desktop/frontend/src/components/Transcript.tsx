import { memo, type CSSProperties, type MouseEvent as ReactMouseEvent, type PointerEvent as ReactPointerEvent, type ReactNode, useCallback, useContext, useEffect, useLayoutEffect, useMemo, useRef, useState, useSyncExternalStore } from "react";
import { useVirtualizer } from "@tanstack/react-virtual";
import type { ControllerLiveStore, Item, LiveStream } from "../lib/useController";
import type { CheckpointMeta } from "../lib/types";
import type { InvocationMetadataMap } from "../lib/invocationDisplay";
import { useT } from "../lib/i18n";
import { AssistantMessage, InvocationMetadataContext, TurnActions, UserMessage } from "./Message";
import { ProcessCompactIcon, ProcessPhaseIcon } from "./ProcessCard";
import { ToolCard } from "./ToolCard";
import { ExtensionCard } from "./ExtensionCard";
import { ArrowDown, ChevronRight, CirclePlay, Info, TriangleAlert } from "lucide-react";
import { Welcome } from "./Welcome";
import { ReadOnlyBatch } from "./ReadOnlyBatch";
import { ToolGroup } from "./ToolGroup";
import { getProcessFoldPreference, onProcessFoldPreferenceChange, type ProcessFoldPreference } from "../lib/processFoldPreference";
import { STEER_NOTICE_PREFIX, isSteerNoticeText } from "../lib/useController";
import { useTranscriptEntranceAnimation } from "../lib/useEntranceAnimation";
import { useTranscriptScrollController } from "../lib/useTranscriptScrollController";
import { useTranscriptSelectionRetention } from "../lib/useTranscriptSelectionRetention";
import { useTranscriptMeasurementInvalidation } from "../lib/useTranscriptMeasurementInvalidation";
import { compactQuestionText, lastQuestionTurn, questionAnchorId, questionTurnsById, scrollVersion, type QuestionAnchor } from "../lib/transcriptGrouping";
import {
  buildTranscriptRows,
  buildTurnModels,
  estimateTranscriptRowSize,
  foldMapWithReasoningOpen,
  foldMapWithToggle,
  foldSegmentStates,
  historyEntryIdForRow,
  reconcileFoldEntries,
  userRowKey,
  EMPTY_FOLDS,
  NO_LIVE,
  type AssistantItem,
  type FoldMap,
  type NoticeItem,
  type SegmentModel,
  type ToolItem,
  type TranscriptLiveFlags,
  type TranscriptRow,
} from "../lib/transcriptRows";
import { observeScrollContentSize } from "../lib/scrollContentObserver";
import { getTranscriptStore } from "../lib/transcriptStore";
import { acquireMarkdownWorkerClient, releaseMarkdownWorkerClient } from "../lib/markdownWorkerClient";
import { noteTranscriptRowCounts } from "../lib/sessionDiagnostics";
import { useReasoningDisplayMode } from "../lib/reasoningDisplayPreference";
import { InlineAssistantReasoning } from "./InlineAssistantReasoning";
import { LiveStreamContext } from "./LiveStreamContext";
import { createTranscriptMeasureElement, estimateCachedTranscriptRowHeight, transcriptHeightCache, transcriptLayoutSignature } from "../lib/transcriptHeightCache";

type OpenTurnAction = { turn: number; menu: "summary" | "rewind" };

const QUESTION_NAV_MIN_COUNT = 2;
type AssistantReasoningDisplay = "normal" | "hide";

const LiveAssistantMessage = memo(function LiveAssistantMessage({
  item,
  defaultExpanded = false,
  expandWhileStreaming = false,
  truncateStreamingReasoning = false,
  creationMode = false,
  reasoningDisplay = "normal",
}: {
  item: AssistantItem;
  defaultExpanded?: boolean;
  expandWhileStreaming?: boolean;
  truncateStreamingReasoning?: boolean;
  creationMode?: boolean;
  reasoningDisplay?: AssistantReasoningDisplay;
}) {
  const live = useContext(LiveStreamContext);
  const shown = useMemo(
    () => {
      const merged =
        live && live.id === item.id
          ? {
              ...item,
              text: live.text,
              reasoning: live.reasoning,
              streaming: true,
              reasoningComplete: live.reasoningComplete,
              reasoningDurationMs:
                live.reasoningStartedAt && live.reasoningCompletedAt && live.reasoningCompletedAt >= live.reasoningStartedAt
                  ? live.reasoningCompletedAt - live.reasoningStartedAt
                  : item.reasoningDurationMs,
            }
          : item;
      if (reasoningDisplay === "hide") {
        return { ...merged, reasoning: "", reasoningComplete: true, reasoningDurationMs: undefined };
      }
      return merged;
    },
    [item, live?.id, live?.text, live?.reasoning, live?.reasoningComplete, live?.reasoningStartedAt, live?.reasoningCompletedAt, reasoningDisplay],
  );
  return (
    <AssistantMessage
      item={shown}
      defaultExpanded={defaultExpanded}
      expandWhileStreaming={expandWhileStreaming}
      truncateStreamingReasoning={truncateStreamingReasoning}
      creationMode={creationMode}
    />
  );
});

const VIRTUAL_OVERSCAN_ROWS = 8;

// ── Helpers ───────────────────────────────────────────────────────────────────

function useTick(on: boolean): number {
  const [, setN] = useState(0);
  useEffect(() => {
    if (!on) return;
    const id = window.setInterval(() => setN((n) => n + 1), 1000);
    return () => window.clearInterval(id);
  }, [on]);
  return Date.now();
}

function formatWorkDuration(durationMs: number, t: ReturnType<typeof useT>): string {
  if (!Number.isFinite(durationMs) || durationMs <= 0) return "";
  const totalSeconds = Math.max(1, Math.round(durationMs / 1000));
  const minutes = Math.floor(totalSeconds / 60);
  const seconds = totalSeconds % 60;
  if (minutes <= 0) return t("transcript.durationSeconds", { s: totalSeconds });
  if (seconds <= 0) return t("transcript.durationMinutes", { m: minutes });
  return t("transcript.durationMinutesSeconds", { m: minutes, s: seconds });
}

function workStatusLabel(durationMs: number, running: boolean, t: ReturnType<typeof useT>): string {
  const duration = formatWorkDuration(durationMs, t);
  if (running) {
    return duration ? t("transcript.workingDuration", { duration }) : t("transcript.working");
  }
  return duration ? t("transcript.workedDuration", { duration }) : t("transcript.worked");
}

function assistantAnswerOnly(item: AssistantItem): AssistantItem {
  return { ...item, reasoning: "", reasoningComplete: true, reasoningDurationMs: undefined };
}

// ── Transcript component ──────────────────────────────────────────────────────

export function Transcript({
  items,
  live: liveProp,
  liveStore,
  tabId,
  footerHeight = 0,
  onPrompt,
  onDeliveryContinue,
  onEditPrompt,
  onRewind,
  checkpoints = [],
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
  olderHistoryCount = 0,
  loadingOlderHistory = false,
  onLoadOlderHistory,
  turnStartAt,
  invocationMetadata = {},
}: {
  items: Item[];
  live?: LiveStream;
  liveStore?: ControllerLiveStore;
  tabId?: string;
  footerHeight?: number;
  onPrompt: (text: string) => void;
  onDeliveryContinue?: () => void;
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
  olderHistoryCount?: number;
  loadingOlderHistory?: boolean;
  onLoadOlderHistory?: () => void;
  turnStartAt?: number;
  invocationMetadata?: InvocationMetadataMap;
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
  const {
    scrollRef,
    stick,
    onScroll,
    onWheelIntent,
    onTouchStartIntent,
    onTouchMoveIntent,
    onKeyScrollIntent,
    isAtBottom,
    scrollToBottomAfterLayout,
    trackQuestions,
    scheduleRepinIfWasPinned,
    resizeFrame,
    lastClientHeight,
    lastFooterHeight,
    setMode: setScrollMode,
    writeOffset,
    resetGeneration,
    canVirtualizerAdjust,
    captureViewportAnchor,
    reconcileViewportAnchor,
  } = useTranscriptScrollController();
  const autoScrollFrame = useRef<number | null>(null);
  const pendingRevealBottomScroll = useRef(false);
  // Creation uses a custom scrollbar (native WebView2 thumb size is unreliable).
  // Thin by default; only thickens when pointer is near the right rail / dragging.
  const [creationScrollbar, setCreationScrollbar] = useState({
    visible: false,
    hot: false,
    thumbTop: 0,
    thumbHeight: 0,
  });
  const creationScrollbarHotRef = useRef(false);
  const creationScrollbarDragRef = useRef<{ pointerId: number; startY: number; startScrollTop: number } | null>(null);
  const SCROLLBAR_HOT_ZONE_PX = 18;
  const SCROLLBAR_MIN_THUMB_PX = 28;

  const syncCreationScrollbarMetrics = useCallback(() => {
    if (!creationMode) return;
    const el = scrollRef.current;
    if (!el) {
      setCreationScrollbar((prev) => (prev.visible || prev.hot ? { visible: false, hot: false, thumbTop: 0, thumbHeight: 0 } : prev));
      return;
    }
    const { scrollTop, scrollHeight, clientHeight } = el;
    const overflow = scrollHeight - clientHeight;
    if (overflow <= 1 || clientHeight <= 0) {
      setCreationScrollbar((prev) => (prev.visible || prev.hot ? { visible: false, hot: false, thumbTop: 0, thumbHeight: 0 } : prev));
      return;
    }
    const thumbHeight = Math.max(SCROLLBAR_MIN_THUMB_PX, Math.round((clientHeight / scrollHeight) * clientHeight));
    const maxThumbTop = Math.max(0, clientHeight - thumbHeight);
    const thumbTop = Math.round((scrollTop / overflow) * maxThumbTop);
    setCreationScrollbar((prev) => {
      if (
        prev.visible &&
        prev.thumbTop === thumbTop &&
        prev.thumbHeight === thumbHeight &&
        prev.hot === creationScrollbarHotRef.current
      ) {
        return prev;
      }
      return {
        visible: true,
        hot: creationScrollbarHotRef.current,
        thumbTop,
        thumbHeight,
      };
    });
  }, [SCROLLBAR_MIN_THUMB_PX, creationMode, scrollRef]);

  const setCreationScrollbarHot = useCallback((next: boolean) => {
    if (creationScrollbarHotRef.current === next) return;
    creationScrollbarHotRef.current = next;
    setCreationScrollbar((prev) => (prev.hot === next ? prev : { ...prev, hot: next }));
  }, []);

  useEffect(() => {
    if (!creationMode) {
      creationScrollbarHotRef.current = false;
      creationScrollbarDragRef.current = null;
      setCreationScrollbar({ visible: false, hot: false, thumbTop: 0, thumbHeight: 0 });
      return;
    }

    const onPointerMove = (event: PointerEvent) => {
      const drag = creationScrollbarDragRef.current;
      const el = scrollRef.current;
      if (drag && el) {
        const overflow = el.scrollHeight - el.clientHeight;
        if (overflow > 0) {
          const thumbHeight = Math.max(SCROLLBAR_MIN_THUMB_PX, Math.round((el.clientHeight / el.scrollHeight) * el.clientHeight));
          const maxThumbTop = Math.max(0, el.clientHeight - thumbHeight);
          const startThumbTop = (drag.startScrollTop / overflow) * maxThumbTop;
          const nextThumbTop = Math.min(maxThumbTop, Math.max(0, startThumbTop + (event.clientY - drag.startY)));
          setScrollMode("programmatic", "custom-scrollbar-drag");
          writeOffset("custom-scrollbar", maxThumbTop > 0 ? (nextThumbTop / maxThumbTop) * overflow : 0);
          syncCreationScrollbarMetrics();
        }
        setCreationScrollbarHot(true);
        return;
      }

      if (!el || el.scrollHeight <= el.clientHeight + 1) {
        setCreationScrollbarHot(false);
        return;
      }
      const rect = el.getBoundingClientRect();
      const inY = event.clientY >= rect.top && event.clientY <= rect.bottom;
      const fromRight = rect.right - event.clientX;
      setCreationScrollbarHot(inY && fromRight >= -2 && fromRight <= SCROLLBAR_HOT_ZONE_PX);
    };

    const endDrag = (event?: PointerEvent) => {
      if (!creationScrollbarDragRef.current) return;
      creationScrollbarDragRef.current = null;
      const el = scrollRef.current;
      if (!el || !event) {
        setCreationScrollbarHot(false);
        return;
      }
      const rect = el.getBoundingClientRect();
      const inY = event.clientY >= rect.top && event.clientY <= rect.bottom;
      const fromRight = rect.right - event.clientX;
      setCreationScrollbarHot(inY && fromRight >= -2 && fromRight <= SCROLLBAR_HOT_ZONE_PX);
    };

    const onPointerUp = (event: PointerEvent) => endDrag(event);
    const onBlur = () => endDrag();

    syncCreationScrollbarMetrics();
    window.addEventListener("pointermove", onPointerMove, { passive: true });
    window.addEventListener("pointerup", onPointerUp, { passive: true });
    window.addEventListener("pointercancel", onPointerUp, { passive: true });
    window.addEventListener("blur", onBlur);
    window.addEventListener("resize", syncCreationScrollbarMetrics);
    return () => {
      window.removeEventListener("pointermove", onPointerMove);
      window.removeEventListener("pointerup", onPointerUp);
      window.removeEventListener("pointercancel", onPointerUp);
      window.removeEventListener("blur", onBlur);
      window.removeEventListener("resize", syncCreationScrollbarMetrics);
      creationScrollbarHotRef.current = false;
      creationScrollbarDragRef.current = null;
      setCreationScrollbar({ visible: false, hot: false, thumbTop: 0, thumbHeight: 0 });
    };
  }, [SCROLLBAR_HOT_ZONE_PX, SCROLLBAR_MIN_THUMB_PX, creationMode, scrollRef, setCreationScrollbarHot, setScrollMode, syncCreationScrollbarMetrics, writeOffset]);

  const handleCreationScroll = useCallback(() => {
    onScroll();
    if (creationMode) syncCreationScrollbarMetrics();
  }, [creationMode, onScroll, syncCreationScrollbarMetrics]);

  useLayoutEffect(() => {
    if (!creationMode) return;
    syncCreationScrollbarMetrics();
  }, [creationMode, items.length, syncCreationScrollbarMetrics]);

  useEffect(() => {
    if (!creationMode || !scrollRef.current) return;
    return observeScrollContentSize(scrollRef.current, syncCreationScrollbarMetrics);
  }, [creationMode, scrollRef, syncCreationScrollbarMetrics]);

  const handleCreationScrollbarThumbPointerDown = useCallback((event: ReactPointerEvent<HTMLDivElement>) => {
    if (!creationMode) return;
    const el = scrollRef.current;
    if (!el) return;
    event.preventDefault();
    event.stopPropagation();
    creationScrollbarDragRef.current = {
      pointerId: event.pointerId,
      startY: event.clientY,
      startScrollTop: el.scrollTop,
    };
    event.currentTarget.setPointerCapture(event.pointerId);
    setCreationScrollbarHot(true);
  }, [creationMode, scrollRef, setCreationScrollbarHot]);

  const handleCreationScrollbarRailPointerDown = useCallback((event: ReactPointerEvent<HTMLDivElement>) => {
    if (!creationMode) return;
    if ((event.target as HTMLElement | null)?.closest?.(".transcript__scrollbar-thumb")) return;
    const el = scrollRef.current;
    if (!el) return;
    const rect = el.getBoundingClientRect();
    const overflow = el.scrollHeight - el.clientHeight;
    if (overflow <= 1) return;
    const thumbHeight = Math.max(SCROLLBAR_MIN_THUMB_PX, Math.round((el.clientHeight / el.scrollHeight) * el.clientHeight));
    const maxThumbTop = Math.max(0, el.clientHeight - thumbHeight);
    const y = event.clientY - rect.top - thumbHeight / 2;
    const nextThumbTop = Math.min(maxThumbTop, Math.max(0, y));
    setScrollMode("programmatic", "custom-scrollbar-rail");
    writeOffset("custom-scrollbar", maxThumbTop > 0 ? (nextThumbTop / maxThumbTop) * overflow : 0);
    syncCreationScrollbarMetrics();
    setCreationScrollbarHot(true);
  }, [SCROLLBAR_MIN_THUMB_PX, creationMode, scrollRef, setCreationScrollbarHot, setScrollMode, syncCreationScrollbarMetrics, writeOffset]);

  const entranceRef = useTranscriptEntranceAnimation<HTMLDivElement>(tabId, revealSignal, items);

  // Lease the markdown parse worker for as long as a transcript surface is
  // mounted; the last release terminates the thread (it re-spawns lazily).
  useEffect(() => {
    acquireMarkdownWorkerClient();
    return () => releaseMarkdownWorkerClient();
  }, []);

  const cancelStreamingAutoScroll = useCallback(() => {
    if (autoScrollFrame.current !== null) {
      cancelAnimationFrame(autoScrollFrame.current);
      autoScrollFrame.current = null;
    }
  }, []);

  const handleWheelIntent = useCallback((event: React.WheelEvent<HTMLElement>) => {
    if (onWheelIntent(event)) cancelStreamingAutoScroll();
  }, [cancelStreamingAutoScroll, onWheelIntent]);

  const handleTouchMoveIntent = useCallback((event: React.TouchEvent<HTMLElement>) => {
    if (onTouchMoveIntent(event)) cancelStreamingAutoScroll();
  }, [cancelStreamingAutoScroll, onTouchMoveIntent]);

  const handleKeyScrollIntent = useCallback((event: React.KeyboardEvent<HTMLElement>) => {
    if (onKeyScrollIntent(event)) cancelStreamingAutoScroll();
  }, [cancelStreamingAutoScroll, onKeyScrollIntent]);

  const questions = useMemo<QuestionAnchor[]>(() => {
    const anchors: QuestionAnchor[] = [];
    let turn = 0;
    for (const it of items) {
      if (it.kind !== "user") continue;
      anchors.push({ id: it.id, text: compactQuestionText(it.text), turn, checkpointTurn: it.checkpointTurn });
      turn += 1;
    }
    return anchors;
  }, [items]);
  const showQuestionNav = questionNavigator && questions.length >= QUESTION_NAV_MIN_COUNT;

  // Track question count and auto-scroll on new messages. A "new question" is
  // a tail append — prepending an older-history page also grows the question
  // list but must not yank the reader to the bottom (the virtualizer's anchor
  // compensation keeps their position instead).
  const questionTailRef = useRef({ length: 0, lastId: "" });
  const newQuestionCount = useRef(0);
  useEffect(() => {
    const lastId = questions[questions.length - 1]?.id ?? "";
    const prev = questionTailRef.current;
    questionTailRef.current = { length: questions.length, lastId };
    if (questions.length > prev.length && lastId !== prev.lastId) {
      newQuestionCount.current += 1;
    }
    trackQuestions(newQuestionCount.current);
  }, [questions, trackQuestions]);

  // Reset the auto-scroll pin when switching tabs so the new session always
  // starts at the bottom. Without this, stick.current from the previous tab
  // persists across React re-renders (Transcript is not keyed by tabId) and
  // disables auto-scroll when the user had scrolled up in the old tab (#4584).
  useEffect(() => {
    resetGeneration(tabId, revealSignal);
    pendingRevealBottomScroll.current = true;
  }, [resetGeneration, revealSignal, tabId]);

  useEffect(() => {
    if (!pendingRevealBottomScroll.current || items.length === 0) return;
    pendingRevealBottomScroll.current = false;
    const frame = requestAnimationFrame(() => {
      scrollToBottomAfterLayout(5);
    });
    return () => cancelAnimationFrame(frame);
  }, [items.length, revealSignal, scrollToBottomAfterLayout, tabId]);

  // Auto-scroll to bottom during streaming. Coalesce fast token/reasoning
  // updates into one layout read/write per animation frame.
  const contentVersion = useMemo(() => scrollVersion(items), [items]);
  useEffect(() => {
    if (items.length === 0) return;
    if (!stick.current) return;
    if (autoScrollFrame.current !== null) return;
    autoScrollFrame.current = requestAnimationFrame(() => {
      autoScrollFrame.current = null;
      if (!stick.current) return;
      const el = scrollRef.current;
      if (el) writeOffset("stream", el.scrollHeight);
    });
  }, [contentVersion, live?.text?.length ?? 0, live?.reasoning?.length ?? 0, writeOffset]);
  useEffect(() => {
    return () => {
      if (autoScrollFrame.current !== null) {
        cancelAnimationFrame(autoScrollFrame.current);
        autoScrollFrame.current = null;
      }
    };
  }, []);

  // ResizeObserver for container height changes.
  useEffect(() => {
    const el = scrollRef.current;
    if (!el || typeof ResizeObserver === "undefined") return;
    lastClientHeight.current = el.clientHeight;
    const observer = new ResizeObserver((entries) => {
      const height = entries[0]?.contentRect.height ?? el.clientHeight;
      const previous = lastClientHeight.current ?? height;
      lastClientHeight.current = height;
      if (items.length === 0) return;
      scheduleRepinIfWasPinned(height - previous, "container-resize");
    });
    observer.observe(el);
    return () => {
      observer.disconnect();
      if (resizeFrame.current !== null) {
        cancelAnimationFrame(resizeFrame.current);
        resizeFrame.current = null;
      }
    };
  }, [items.length, scheduleRepinIfWasPinned]);

  // Footer height changes → frame-batched scroll repin.
  useEffect(() => {
    const el = scrollRef.current;
    if (!el) return;
    const previous = lastFooterHeight.current ?? footerHeight;
    lastFooterHeight.current = footerHeight;
    if (items.length === 0) return;
    scheduleRepinIfWasPinned(previous - footerHeight, "footer-resize");
  }, [footerHeight, items.length, scheduleRepinIfWasPinned]);

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
  const segmentStates = useMemo(() => foldSegmentStates(turnModels), [turnModels]);

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
    setFolds((prev) => foldMapWithToggle(prev, segmentKey, currentlyOpen));
  }, []);

  const handleReasoningManualOpen = useCallback((segmentKey: string) => {
    const running = segmentStates.find((segment) => segment.key === segmentKey)?.hasRunningWork ?? false;
    setFolds((prev) => foldMapWithReasoningOpen(prev, segmentKey, running));
  }, [segmentStates]);

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

  const userTurn = useMemo(() => questionTurnsById(questions), [questions]);
  const lastTurn = useMemo(() => lastQuestionTurn(questions, userTurn), [questions, userTurn]);
  const checkpointsByTurn = useMemo(() => new Map(checkpoints.map((checkpoint) => [checkpoint.turn, checkpoint])), [checkpoints]);
  const hasCheckpointForTurn = useCallback((turn: number) => checkpointsByTurn.has(turn), [checkpointsByTurn]);

  const turnForUser = useCallback((item: Extract<Item, { kind: "user" }>) => userTurn.get(item.id), [userTurn]);
  const rows = useMemo(
    () => buildTranscriptRows(turnModels, { folds, foldPreference, hasOlderHistory, creationMode, turnForUser, hasCheckpointForTurn }),
    [turnModels, folds, foldPreference, hasOlderHistory, creationMode, turnForUser, hasCheckpointForTurn],
  );
  const rowIndexByKey = useMemo(() => {
    const map = new Map<string, number>();
    rows.forEach((row, index) => map.set(String(row.key), index));
    return map;
  }, [rows]);

  const selectionRetention = useTranscriptSelectionRetention({
    tabId,
    revealSignal,
    rowIndexByKey,
    setScrollMode,
    cancelStreamingScroll: cancelStreamingAutoScroll,
    captureViewportAnchor,
    reconcileViewportAnchor,
  });

  const getRowKey = useCallback((index: number) => `${tabId ?? ""}:${String(rows[index]?.key ?? index)}`, [rows, tabId]);
  const estimateRowSize = useCallback((index: number) => {
    const row = rows[index];
    const signature = transcriptLayoutSignature(scrollRef.current);
    const cached = transcriptHeightCache.get(tabId ?? "", signature, String(row?.key ?? index));
    if (cached != null) return cached;
    return estimateCachedTranscriptRowHeight(row, scrollRef.current?.clientWidth ?? 0, estimateTranscriptRowSize(row));
  }, [rows, scrollRef, tabId]);
  const measureRowSize = useMemo(() => createTranscriptMeasureElement({
    tabId: tabId ?? "",
    getLayoutElement: () => scrollRef.current,
  }), [scrollRef, tabId]);
  const virtualizer = useVirtualizer({
    count: rows.length,
    getScrollElement: () => scrollRef.current,
    getItemKey: getRowKey,
    estimateSize: estimateRowSize,
    measureElement: measureRowSize,
    overscan: VIRTUAL_OVERSCAN_ROWS,
    rangeExtractor: selectionRetention.rangeExtractor,
    // Key-anchored compensation: prepended history pages, fold toggles and
    // async row growth restore the scroll position of the anchor row.
    anchorTo: "end",
    scrollToFn: (offset, options) => {
      writeOffset("virtualizer", offset + (options.adjustments ?? 0), options.behavior ?? "auto");
    },
    // Measurement callbacks can arrive during React's commit phase. Let the
    // virtualizer update stable row positions directly instead of dispatching a
    // reducer update for every ResizeObserver measurement (React #185).
    directDomUpdates: true,
    // Batch ResizeObserver measurements into one layout read per frame.
    useAnimationFrameWithResizeObserver: true,
  });
  virtualizer.shouldAdjustScrollPositionOnItemSizeChange = () => canVirtualizerAdjust();
  useTranscriptMeasurementInvalidation({ scrollRef, virtualizer, selectionActive: selectionRetention.active });

  const sizerRef = useCallback(
    (el: HTMLDivElement | null) => {
      virtualizer.containerRef(el);
      entranceRef.current = el;
    },
    [virtualizer, entranceRef],
  );
  const virtualItems = virtualizer.getVirtualItems();
  useEffect(() => {
    noteTranscriptRowCounts(virtualItems.length, rows.length);
  }, [virtualItems.length, rows.length]);

  // ── JumpBar integration ───────────────────────────────────────────────────
  const handleJumpToQuestion = useCallback((question: QuestionAnchor) => {
    const index = rowIndexByKey.get(String(userRowKey(question.id)));
    if (index == null) return;
    stick.current = false;
    setScrollMode("programmatic", "jump");
    virtualizer.scrollToIndex(index, { align: "start", behavior: "smooth" });
  }, [rowIndexByKey, setScrollMode, stick, virtualizer]);

  // After a non-fork rewind, scroll to the last user message (the
  // rewound-to point) so the user knows where they are.
  useEffect(() => {
    if (rewindSignal <= 0 || questions.length === 0) return;
    const lastQ = questions[questions.length - 1];
    const index = rowIndexByKey.get(String(userRowKey(lastQ.id)));
    if (index == null) return;
    stick.current = false;
    setScrollMode("programmatic", "rewind");
    virtualizer.scrollToIndex(index, { align: "start" });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [rewindSignal]);

  const empty = items.length === 0;

  useLayoutEffect(() => {
    if (!empty) return;
    const el = scrollRef.current;
    if (!el) return;
    setScrollMode("programmatic", "empty-transcript");
    writeOffset("jump-bottom", 0);
    stick.current = false;
    const frame = requestAnimationFrame(() => {
      writeOffset("jump-bottom", 0);
    });
    return () => cancelAnimationFrame(frame);
  }, [empty, scrollRef, setScrollMode, stick, tabId, writeOffset]);

  // ── Row rendering ─────────────────────────────────────────────────────────
  const renderRow = (row: TranscriptRow): ReactNode => {
    switch (row.kind) {
      case "older-history":
        return (
          <button
            type="button"
            className="warm-collapse transcript__older"
            onClick={onLoadOlderHistory}
            disabled={loadingOlderHistory}
          >
            {loadingOlderHistory ? t("common.loading") : t("transcript.showEarlierHistory", { n: olderHistoryCount })}
          </button>
        );
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
            <InlineAssistantReasoning item={row.item} onManualOpen={() => handleReasoningManualOpen(row.segmentKey)} />
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
            truncateStreamingReasoning={true}
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
            onAction={row.item.action === "continue_delivery" ? (onDeliveryContinue ?? (() => onPrompt(t("notice.deliveryIncompleteContinuePrompt")))) : undefined}
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
  };

  // ── Assemble rendered output ──────────────────────────────────────────────
  return (
    <InvocationMetadataContext.Provider value={invocationMetadata}>
    <div className="transcript-shell">
      <div
        className={`transcript${empty ? " transcript--empty" : ""}${creationMode ? " transcript--creation-scrollbar" : ""}${creationMode && creationScrollbar.hot ? " transcript--scrollbar-hot" : ""}`}
        ref={scrollRef}
        onScroll={creationMode ? handleCreationScroll : onScroll}
        onWheelCapture={handleWheelIntent}
        onTouchStartCapture={onTouchStartIntent}
        onTouchMoveCapture={handleTouchMoveIntent}
        onKeyDownCapture={handleKeyScrollIntent}
        onPointerDownCapture={selectionRetention.onPointerDownCapture}
      >
        {empty && !hydrating && <Welcome onPrompt={onPrompt} variant={welcomeVariant} />}

        <LiveStreamContext.Provider value={live}>
          <div ref={sizerRef} className="transcript__virtual-sizer">
            {virtualItems.map((virtualRow) => {
              const row = rows[virtualRow.index];
              if (!row) return null;
              return (
                <TranscriptRowShell
                  key={virtualRow.key}
                  index={virtualRow.index}
                  row={row}
                  measureElement={virtualizer.measureElement}
                  tabId={tabId}
                >
                  {renderRow(row)}
                </TranscriptRowShell>
              );
            })}
          </div>
        </LiveStreamContext.Provider>
      </div>

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
        <QuestionJumpBar questions={questions} onJump={handleJumpToQuestion} />
      )}

      {!empty && !isAtBottom && (
        <button
          type="button"
          className="transcript__jump-bottom"
          onClick={() => scrollToBottomAfterLayout(2)}
          aria-label={t("transcript.jumpToBottom")}
          title={t("transcript.jumpToBottom")}
        >
          <ArrowDown size={18} strokeWidth={2.2} aria-hidden="true" />
        </button>
      )}
    </div>
    </InvocationMetadataContext.Provider>
  );
}

// ── Virtual row shell ─────────────────────────────────────────────────────────
// Owns the measured wrapper element. Mounting implies the row is in or near
// the viewport (overscan), so history-backed rows resolve their lazy
// full-content refs here — the transcript store patches the item by stable id
// and the row re-renders in place.

function TranscriptRowShell({
  index,
  row,
  measureElement,
  tabId,
  children,
}: {
  index: number;
  row: TranscriptRow;
  measureElement: (element: HTMLDivElement | null) => void;
  tabId?: string;
  children: ReactNode;
}) {
  const entryId = historyEntryIdForRow(row);
  useEffect(() => {
    if (entryId) getTranscriptStore().requestEntryFullContent(tabId, entryId);
  }, [entryId, tabId]);
  return (
    <div data-index={index} data-row-key={String(row.key)} ref={measureElement} className="transcript__row">
      {children}
    </div>
  );
}

// ── ProcessFoldHeader: the fold header row of one process segment ────────────
// The fold body is NOT rendered here: an open fold contributes its body rows
// to the virtual row model (they mount only when scrolled into view), a closed
// fold builds no React subtree at all.

function ProcessFoldHeader({
  segment,
  open,
  onToggle,
  turnStartAt,
}: {
  segment: SegmentModel;
  open: boolean;
  onToggle: () => void;
  turnStartAt?: number;
}) {
  const t = useT();
  const live = useContext(LiveStreamContext);
  const displayItems = segment.displayItems;

  const hasRunningWork = segment.hasRunningWork;
  const now = useTick(hasRunningWork);
  const runningDurationMs = hasRunningWork
    ? turnStartAt
      ? Math.max(0, now - turnStartAt)
      : live?.reasoningStartedAt
        ? Math.max(0, now - live.reasoningStartedAt)
        : 0
    : 0;
  const effectiveDurationMs = hasRunningWork ? Math.max(segment.durationMs, runningDurationMs) : segment.durationMs;

  const baseLabel = workStatusLabel(effectiveDurationMs, hasRunningWork, t);
  // Surface what the closed fold hides — a bare duration reads as pure timing
  // and users have no way to know process detail sits behind it.
  const toolCount = displayItems.reduce((n, it) => n + (it.kind === "tool" ? 1 : 0), 0);
  const thoughtCount = displayItems.reduce((n, it) => n + (it.kind === "assistant" ? 1 : 0), 0);
  const countParts: string[] = [];
  if (toolCount > 0) countParts.push(t("transcript.toolCount", { n: toolCount }));
  if (thoughtCount > 0) countParts.push(t("transcript.thoughtCount", { n: thoughtCount }));
  const label = segment.labelStyle === "counts"
    ? (countParts.length > 0 ? countParts.join(" · ") : t("transcript.processed"))
    : countParts.length > 0
      ? `${baseLabel} · ${countParts.join(" · ")}`
      : baseLabel;
  return (
    <div className={`turn-collapse${open ? " turn-collapse--open" : ""}`} data-kind="reasoning" data-entrance={displayItems[0]?.id || undefined}>
      <button
        type="button"
        className="reasoning__head"
        onClick={onToggle}
        aria-expanded={open}
      >
        <span className="turn-collapse__label" data-creation-label={label}>{label}</span>
        {!hasRunningWork && <ChevronRight className={`reasoning__chevron${open ? " reasoning__chevron--open" : ""}`} size={12} />}
      </button>
    </div>
  );
}

// ── JumpBar, PhaseCard, NoticeCard, CompactionCard ────────────────────────────

function QuestionJumpBar({ questions, onJump }: { questions: QuestionAnchor[]; onJump: (question: QuestionAnchor) => void }) {
  const t = useT();
  const [hovered, setHovered] = useState<number | null>(null);
  const [active, setActive] = useState<number | null>(null);
  const barRef = useRef<HTMLDivElement>(null);
  const previewTop = useRef(0);
  const [showPreview, setShowPreview] = useState(false);

  useEffect(() => {
    if (questions.length === 0) return;
    setActive(questions[questions.length - 1]?.turn ?? null);
  }, [questions]);

  useEffect(() => {
    if (active === null) return;
    const el = barRef.current?.querySelector(`[data-turn="${active}"]`);
    el?.scrollIntoView({ block: "nearest" });
  }, [active]);

  const hoverIdx = hovered !== null ? questions.findIndex((question) => question.turn === hovered) : -1;
  const hoveredQuestion = hovered !== null ? questions.find((question) => question.turn === hovered) : undefined;

  const closestQuestionFromY = (clientY: number): { question: QuestionAnchor; previewY: number } | null => {
    const el = barRef.current;
    if (!el) return null;
    const markers = el.querySelectorAll<HTMLElement>(".jump-item");
    const barRect = el.getBoundingClientRect();
    let closest = -1;
    let closestDist = Infinity;
    let closestY = 0;
    markers.forEach((item, index) => {
      const rect = item.getBoundingClientRect();
      const midY = rect.top + rect.height / 2;
      const dist = Math.abs(clientY - midY);
      if (dist < closestDist) {
        closestDist = dist;
        closest = index;
        closestY = midY - barRect.top;
      }
    });
    const question = questions[closest];
    if (!question) return null;
    return { question, previewY: closestY };
  };

  const onMove = (e: ReactMouseEvent<HTMLDivElement>) => {
    const closest = closestQuestionFromY(e.clientY);
    if (!closest) return;
    previewTop.current = closest.previewY;
    setHovered(closest.question.turn);
    setShowPreview(true);
  };

  const scrollTo = (question: QuestionAnchor) => {
    setActive(question.turn);
    onJump(question);
  };

  const onRailMouseDown = (e: ReactMouseEvent<HTMLDivElement>) => {
    const closest = closestQuestionFromY(e.clientY);
    if (!closest) return;
    e.preventDefault();
    previewTop.current = closest.previewY;
    setHovered(closest.question.turn);
    setShowPreview(true);
    scrollTo(closest.question);
  };

  const onItemMouseDown = (e: ReactMouseEvent<HTMLButtonElement>, question: QuestionAnchor) => {
    e.preventDefault();
    scrollTo(question);
  };

  const dotProps = (
    idx: number,
    turn: number,
  ): { style: CSSProperties; "data-d"?: string } => {
    const isActive = active === turn;
    if (hoverIdx < 0) {
      return { style: { width: isActive ? 18 : 12, background: isActive ? "var(--accent)" : undefined } };
    }
    const d = Math.abs(idx - hoverIdx);
    const width = d === 0 ? 32 : d === 1 ? 20 : d === 2 ? 14 : isActive ? 18 : 12;
    const background = d <= 2 ? undefined : isActive ? "var(--accent)" : undefined;
    return {
      style: { width, transitionDelay: `${d * 20}ms`, background },
      "data-d": d <= 2 ? String(d) : undefined,
    };
  };

  return (
    <nav
      className="jump-bar"
      ref={barRef}
      aria-label={t("questionNav.label")}
      onMouseMove={onMove}
      onMouseLeave={() => {
        setHovered(null);
        setShowPreview(false);
      }}
    >
      <div className="jump-scroll" onMouseDown={onRailMouseDown} onClick={onRailMouseDown}>
        {questions.map((question, index) => (
          <button
            className="jump-item"
            key={question.id}
            type="button"
            data-turn={question.turn}
            aria-label={t("questionNav.jump", { n: question.turn + 1 })}
            onMouseDown={(e) => onItemMouseDown(e, question)}
            onClick={(e) => {
              e.stopPropagation();
              if (e.detail === 0) scrollTo(question);
            }}
          >
            <span className="jump-dot" {...dotProps(index, question.turn)} />
          </button>
        ))}
      </div>
      {showPreview && hoveredQuestion && (
        <div className="jump-preview" style={{ top: previewTop.current }} role="tooltip">
          <span className="jump-text">{hoveredQuestion.text}</span>
        </div>
      )}
    </nav>
  );
}

type CompactionItem = Extract<Item, { kind: "compaction" }>;

function PhaseCard({ id, text }: { id: string; text: string }) {
  return <div className="phase" data-entrance={id}><ProcessPhaseIcon size={12} /><span>{text}</span></div>;
}

// A mid-turn steer is the user's own message, so it renders on the user side
// of the transcript instead of disappearing into the work fold.
function SteerCard({ id, text }: { id: string; text: string }) {
  const t = useT();
  const body = text.startsWith(STEER_NOTICE_PREFIX) ? text.slice(STEER_NOTICE_PREFIX.length) : text;
  return (
    <div className="steer-line" data-entrance={id}>
      <div className="steer-line__bubble" title={t("transcript.steer")}>
        <span className="steer-line__icon" aria-hidden="true">↪</span>
        <span className="steer-line__text">{body}</span>
      </div>
    </div>
  );
}

function DecisionReceiptLine({ receipt }: { receipt: NonNullable<NoticeItem["decisionReceipt"]> }) {
  const t = useT();
  const titleKey = receipt.kind === "ask"
    ? "notice.decisionReceiptAsk"
    : receipt.kind === "plan"
    ? "notice.decisionReceiptPlan"
    : receipt.kind === "recovery"
    ? "notice.decisionReceiptRecovery"
    : "notice.decisionReceiptTool";
  const outcomeKeys: Record<string, string> = {
    allow_once: "notice.decisionAllowOnce",
    allow_session: "notice.decisionAllowSession",
    allow_persistent: "notice.decisionAllowPersistent",
    deny: "notice.decisionDeny",
    start_execution: "notice.decisionStartExecution",
    revise_plan: "notice.decisionRevisePlan",
    exit_plan: "notice.decisionExitPlan",
    recovery_continue: "notice.decisionRecoveryContinue",
    recovery_continue_task: "notice.decisionRecoveryContinueTask",
    recovery_revise: "notice.decisionRecoveryRevise",
    answered: "notice.decisionAnswered",
  };
  const outcome = outcomeKeys[receipt.outcome]
    ? t(outcomeKeys[receipt.outcome] as never)
    : receipt.outcome || t("notice.decisionReceiptTitle");
  const showOutcome = receipt.kind !== "ask" || receipt.outcome !== "answered";
  return (
    <div className="notice-line__decision-receipt">
      <span className="notice-line__decision-title">{t(titleKey as never)}</span>
      {showOutcome && <span className="notice-line__decision-outcome">{outcome}</span>}
      {receipt.tool && <code>{receipt.tool}</code>}
      {receipt.subject && <span className="notice-line__decision-subject">{receipt.subject}</span>}
    </div>
  );
}

export function NoticeCard({ item, onAction, actionDisabled = false }: { item: NoticeItem; onAction?: () => void; actionDisabled?: boolean }) {
  const t = useT();
  const StatusIcon = item.level === "warn" ? TriangleAlert : Info;
  return (
    <div className={`notice-line notice-line--${item.level}${item.variant ? ` notice-line--${item.variant}` : ""}`} data-entrance={item.id}>
      <StatusIcon className="notice-line__icon" size={14} aria-hidden="true" />
      <div className="notice-line__text">
        {item.decisionReceipt ? (
          <DecisionReceiptLine receipt={item.decisionReceipt} />
        ) : (
          <>
            {item.title ? <div className="notice-line__title">{item.title}</div> : null}
            <div className="notice-line__body">{item.text}</div>
          </>
        )}
        {item.action && onAction ? (
          <div className="notice-line__actions">
            <button className="btn btn--small" type="button" onClick={onAction} disabled={actionDisabled}>
              <CirclePlay size={13} aria-hidden="true" />
              <span>{t("notice.deliveryIncompleteContinue")}</span>
            </button>
          </div>
        ) : null}
        {item.detail ? (
          <details className="notice-line__details">
            <summary>{t("notice.details")}</summary>
            <div>{item.detail}</div>
          </details>
        ) : null}
      </div>
    </div>
  );
}

function CompactionCard({ item }: { item: CompactionItem }) {
  const t = useT();
  const [open, setOpen] = useState(false);
  if (item.pending) {
    return <div className="compaction compaction--pending" data-entrance={item.id}><ProcessCompactIcon size={12} /><span>{t("compaction.working")}</span></div>;
  }
  return (
    <div className="compaction" data-entrance={item.id}>
      <button type="button" className="compaction__head" onClick={() => setOpen((v) => !v)} aria-expanded={open}>
        <ProcessCompactIcon size={12} />
        <span>{t("compaction.title")}</span>
        <span className="compaction__meta">{t("compaction.messages", { n: item.messages })}{item.trigger ? ` · ${item.trigger}` : ""}</span>
        <ChevronRight className={open ? "compaction__chevron--open" : ""} size={12} />
      </button>
      {open && <pre className="compaction__body">{item.summary}</pre>}
    </div>
  );
}
