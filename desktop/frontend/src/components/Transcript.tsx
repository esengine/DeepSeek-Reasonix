import { createContext, memo, type CSSProperties, type MouseEvent as ReactMouseEvent, type PointerEvent as ReactPointerEvent, type ReactNode, useCallback, useContext, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import type { Item, LiveStream } from "../lib/useController";
import type { CheckpointMeta } from "../lib/types";
import type { InvocationMetadataMap } from "../lib/invocationDisplay";
import { useT } from "../lib/i18n";
import { AssistantMessage, InvocationMetadataContext, TurnActions, UserMessage } from "./Message";
import { ProcessBrainIcon, ProcessCompactIcon, ProcessPhaseIcon } from "./ProcessCard";
import { ToolCard } from "./ToolCard";
import { ArrowDown, ChevronRight, CirclePlay, Info, TriangleAlert } from "lucide-react";
import { Welcome } from "./Welcome";
import { ReadOnlyBatch } from "./ReadOnlyBatch";
import { ToolGroup, isCreationGroupableTool, toolGroupKind, type ToolGroupKind } from "./ToolGroup";
import { getDisplayMode, onDisplayModeChange, type DisplayMode } from "../lib/displayMode";
import { getProcessFoldPreference, onProcessFoldPreferenceChange, type ProcessFoldPreference } from "../lib/processFoldPreference";
import { STEER_NOTICE_PREFIX, isReadOnlyTool, isSteerNoticeText } from "../lib/useController";
import { useGSAPCollapse } from "../lib/useGSAPCollapse";
import { useEntranceAnimation } from "../lib/useEntranceAnimation";
import { useScrollManager } from "../lib/useScrollManager";
import { buildTurnGroups, compactQuestionText, createWarmLayerState, lastQuestionTurn, questionAnchorId, questionTurnsById, scrollVersion, warmColdPageForTurn, warmLayerWithColdPageAtLeast, warmLayerWithExpandedTurn, warmLayerWithNextColdPage, warmPagination, warmUserPreview, type QuestionAnchor, type TurnGroup, type WarmLayerState } from "../lib/transcriptGrouping";
import { appendTurnActionCopyText } from "../lib/turnActionCopy";
import { displayReasoningText } from "../lib/reasoningDisplay";
import { observeScrollContentSize } from "../lib/scrollContentObserver";

type ToolItem = Extract<Item, { kind: "tool" }>;
type AssistantItem = Extract<Item, { kind: "assistant" }>;
type NoticeItem = Extract<Item, { kind: "notice" }>;
type OpenTurnAction = { turn: number; menu: "summary" | "rewind" };

const QUESTION_NAV_MIN_COUNT = 2;
const LiveStreamContext = createContext<LiveStream | undefined>(undefined);
type AssistantReasoningDisplay = "normal" | "hide";

const LiveAssistantMessage = memo(function LiveAssistantMessage({
  item,
  defaultExpanded = false,
  expandWhileStreaming = true,
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

function InlineAssistantReasoning({ item }: { item: AssistantItem }) {
  const t = useT();
  const live = useContext(LiveStreamContext);
  const [open, setOpen] = useState(true);
  const bodyRef = useRef<HTMLDivElement>(null);
  useGSAPCollapse(bodyRef, open);
  const shown = live && live.id === item.id
    ? {
        reasoning: live.reasoning,
        streaming: true,
        reasoningComplete: live.reasoningComplete,
      }
    : item;
  const reasoning = shown.reasoning.trim();
  if (!reasoning) return null;
  const visibleReasoning = displayReasoningText(shown.reasoning, {
    streaming: shown.streaming,
    truncateStreaming: true,
  });
  const running = shown.streaming && !shown.reasoningComplete;
  return (
    <div className={`turn-collapse__reasoning-phase${open ? " turn-collapse__reasoning-phase--open" : ""}`}>
      <button
        type="button"
        className="turn-collapse__reasoning-head"
        data-running={running ? "" : undefined}
        onClick={() => setOpen((v) => !v)}
        aria-expanded={open}
      >
        <ProcessBrainIcon size={12} />
        <span>{running ? t("msg.thinkingRunning") : t("msg.thinking")}</span>
        <ChevronRight className={`reasoning__chevron${open ? " reasoning__chevron--open" : ""}`} size={12} />
      </button>
      <div ref={bodyRef} className="turn-collapse__inline-reasoning">{visibleReasoning}</div>
    </div>
  );
}

// ── Layer budgets ─────────────────────────────────────────────────────────────
// Hot zone: the most recent N user turns are always fully rendered. All data
// stays in memory (items[]), so expanding a warm turn is instant — no API call.
// Cold zone: a "load more" button paginates the warm zone in batches.
//
//   items[0]  ─┐
//   ...        │ Cold zone  ───  paginated, shown on "load more"
//              ├────────────  warmTurnStart
//   ...        │ Warm zone  ───  collapsible summary cards (individual expand)
//              ├────────────  hotStartIdx
//   items[N]  ─┤ Hot zone   ───  fully rendered
//   ...        │
//   items[end] ┘

const HOT_TURNS = 30;
const WARM_PAGE_SIZE = 20; // cold-zone pagination batch

// ── Helpers ───────────────────────────────────────────────────────────────────

function turnWorkDurationMs(items: readonly Item[]): number {
  const persisted = items.reduce((ms, it) => {
    if (it.kind !== "assistant") return ms;
    return Math.max(ms, it.workDurationMs ?? 0);
  }, 0);
  if (persisted > 0) return persisted;
  return items.reduce((ms, it) => {
    if (it.kind === "tool") return ms + (it.durationMs ?? 0);
    if (it.kind === "assistant") return ms + (it.reasoningDurationMs ?? 0);
    return ms;
  }, 0);
}

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

function assistantReasoningOnly(item: AssistantItem): AssistantItem {
  return { ...item, text: "" };
}

function assistantAnswerOnly(item: AssistantItem): AssistantItem {
  return { ...item, reasoning: "", reasoningComplete: true, reasoningDurationMs: undefined };
}

function assistantHasVisibleAnswer(item: AssistantItem, liveId: string | undefined, liveHasAnswerText: boolean): boolean {
  if (item.text.trim() !== "") return true;
  return liveId === item.id && liveHasAnswerText;
}

type TurnDisplayParts = {
  processItems: Item[];
  outsideItems: Array<NoticeItem | AssistantItem>;
};

// Splits a turn by channel, not by position: reasoning, tools, phases, info
// notices, and compaction cards are process material and fold; every assistant
// message with answer text is model output addressed to the user and stays
// outside the fold. Warnings must survive the fold auto-closing on completion,
// and steers are the user's own words — neither belongs to the model's work
// process.
//
// The turn is returned as ordered segments so the conversation keeps its real
// timeline: process that ran after an answer or steer opens a new segment
// (and thus a new fold) instead of being pulled ahead of it. Warn notices and
// delivery status cards stay visible but do not split the fold — a mid-turn
// warning is not a conversational boundary, and a delivery pause must keep its
// continue action reachable instead of collapsing with the process items.
function partitionTurnItems(
  items: readonly Item[],
  liveId?: string,
  liveHasAnswerText = false,
  liveHasReasoning = false,
): TurnDisplayParts[] {
  const segments: TurnDisplayParts[] = [];
  let current: TurnDisplayParts = { processItems: [], outsideItems: [] };
  let currentHasConversation = false;
  const flushSegment = () => {
    if (current.processItems.length === 0 && current.outsideItems.length === 0) return;
    segments.push(current);
    current = { processItems: [], outsideItems: [] };
    currentHasConversation = false;
  };
  const pushProcess = (item: Item) => {
    if (currentHasConversation) flushSegment();
    current.processItems.push(item);
  };
  for (const item of items) {
    if (item.kind === "user") continue;
    if (item.kind === "notice") {
      if (isSteerNoticeText(item.text)) {
        current.outsideItems.push(item);
        currentHasConversation = true;
      } else if (item.level === "warn" || item.variant === "delivery") {
        current.outsideItems.push(item);
      } else {
        pushProcess(item);
      }
      continue;
    }
    if (item.kind !== "assistant") {
      pushProcess(item);
      continue;
    }
    const hasReasoning = Boolean(item.reasoning || (liveId === item.id && liveHasReasoning));
    if (assistantHasVisibleAnswer(item, liveId, liveHasAnswerText)) {
      if (hasReasoning) pushProcess(assistantReasoningOnly(item));
      current.outsideItems.push(item);
      currentHasConversation = true;
      continue;
    }
    if (hasReasoning) pushProcess(item);
  }
  flushSegment();
  return segments;
}

// ── Transcript component ──────────────────────────────────────────────────────

export function Transcript({
  items,
  live,
  tabId,
  footerHeight = 0,
  onPrompt,
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
  tabId?: string;
  footerHeight?: number;
  onPrompt: (text: string) => void;
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
  const {
    scrollRef,
    stick,
    onScroll,
    onWheelIntent,
    onTouchStartIntent,
    onTouchMoveIntent,
    onKeyScrollIntent,
    isAtBottom,
    smoothScrollTo,
    scrollToBottomAfterLayout,
    trackQuestions,
    scheduleRepinIfWasPinned,
    resizeFrame,
    lastClientHeight,
    lastFooterHeight,
  } = useScrollManager();
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
          el.scrollTop = maxThumbTop > 0 ? (nextThumbTop / maxThumbTop) * overflow : 0;
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
  }, [SCROLLBAR_HOT_ZONE_PX, SCROLLBAR_MIN_THUMB_PX, creationMode, scrollRef, setCreationScrollbarHot, syncCreationScrollbarMetrics]);

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
    el.scrollTop = maxThumbTop > 0 ? (nextThumbTop / maxThumbTop) * overflow : 0;
    syncCreationScrollbarMetrics();
    setCreationScrollbarHot(true);
  }, [SCROLLBAR_MIN_THUMB_PX, creationMode, scrollRef, setCreationScrollbarHot, syncCreationScrollbarMetrics]);

  const pendingQuestionJump = useRef<QuestionAnchor | null>(null);
  const sessionKey = useMemo(() => `${items[0]?.id ?? ""}|${items[items.length - 1]?.id ?? ""}`, [items]);
  const warmLayerSessionKey = useMemo(() => `${tabId ?? ""}|${revealSignal}|${items[0]?.id ?? ""}`, [items, revealSignal, tabId]);
  const entranceRef = useEntranceAnimation<HTMLDivElement>(sessionKey, items.length);

  const [displayMode, setDisplayMode] = useState<DisplayMode>(() => getDisplayMode());
  useEffect(() => onDisplayModeChange((mode) => setDisplayMode(mode)), []);

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

  // Track question count and auto-scroll on new messages.
  useEffect(() => { trackQuestions(questions.length); }, [questions.length, trackQuestions]);

  // Reset the auto-scroll pin when switching tabs so the new session always
  // starts at the bottom. Without this, stick.current from the previous tab
  // persists across React re-renders (Transcript is not keyed by tabId) and
  // disables auto-scroll when the user had scrolled up in the old tab (#4584).
  useEffect(() => {
    stick.current = true;
    pendingRevealBottomScroll.current = true;
  }, [tabId, revealSignal]);

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
      if (el) el.scrollTop = el.scrollHeight;
    });
  }, [contentVersion, live?.text?.length ?? 0, live?.reasoning?.length ?? 0]);
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
      scheduleRepinIfWasPinned(height - previous);
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

  // Footer height changes → smooth scroll repin with GSAP.
  useEffect(() => {
    const el = scrollRef.current;
    if (!el) return;
    const previous = lastFooterHeight.current ?? footerHeight;
    lastFooterHeight.current = footerHeight;
    if (items.length === 0) return;
    scheduleRepinIfWasPinned(previous - footerHeight);
  }, [footerHeight, items.length, scheduleRepinIfWasPinned]);

  // After a non-fork rewind, scroll to the last user message (the
  // rewound-to point) so the user knows where they are.
  useEffect(() => {
    if (rewindSignal <= 0 || questions.length === 0) return;
    const lastQ = questions[questions.length - 1];
    const el = document.getElementById(questionAnchorId(lastQ.id));
    if (!el || !scrollRef.current) return;
    stick.current = false;
    scrollRef.current.scrollTop = el.offsetTop - scrollRef.current.offsetTop - 12;
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [rewindSignal]);

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

  // ── Layer state ────────────────────────────────────────────────────────────
  const [warmLayerState, setWarmLayerState] = useState<WarmLayerState>(() => createWarmLayerState(warmLayerSessionKey));
  const defaultWarmLayerState = useMemo<WarmLayerState>(() => createWarmLayerState(warmLayerSessionKey), [warmLayerSessionKey]);
  const activeWarmLayerState = warmLayerState.sessionKey === warmLayerSessionKey
    ? warmLayerState
    : defaultWarmLayerState;
  const { expandedWarmTurns, coldPage } = activeWarmLayerState;

  // Compute turn groups from the structural item list. Streaming text updates
  // keep the same items[] reference, so this stays out of the token hot path.
  const turnGroups = useMemo(() => buildTurnGroups(items), [items]);

  // hotStartIdx: first index of the hot zone in items[].
  const hotStartIdx = useMemo(() => {
    let needed = HOT_TURNS;
    for (let i = items.length - 1; i >= 0; i--) {
      if (items[i].kind === "user") {
        needed--;
        if (needed <= 0) return i;
      }
    }
    return 0;
  }, [items]);

  // How many turns are in the cold zone (not yet shown).
  const { warmStartTurn, warmEndTurn, coldTurnCount } = useMemo(
    () => warmPagination({ turnCount: turnGroups.length, hotTurns: HOT_TURNS, pageSize: WARM_PAGE_SIZE, coldPage }),
    [coldPage, turnGroups.length],
  );

  useLayoutEffect(() => {
    const question = pendingQuestionJump.current;
    if (!question) return;
    const node = document.getElementById(questionAnchorId(question.id));
    if (!node) return;
    pendingQuestionJump.current = null;
    stick.current = false;
    smoothScrollTo(node, 12);
  }, [expandedWarmTurns, smoothScrollTo, stick, warmStartTurn]);

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

  // ── JumpBar integration ───────────────────────────────────────────────────
  const jumpToQuestion = (question: QuestionAnchor) => {
    const node = document.getElementById(questionAnchorId(question.id));
    if (!node) return;
    pendingQuestionJump.current = null;
    stick.current = false;
    smoothScrollTo(node, 12);
  };

  const handleJumpToQuestion = useCallback((question: QuestionAnchor) => {
    pendingQuestionJump.current = question;
    // Auto-expand the warm turn when jumping to an old question.
    const warmTurnStart = turnGroups.length - HOT_TURNS;
    if (question.turn < warmTurnStart) {
      const neededColdPage = warmColdPageForTurn({
        turn: question.turn,
        turnCount: turnGroups.length,
        hotTurns: HOT_TURNS,
        pageSize: WARM_PAGE_SIZE,
      });
      setWarmLayerState((prev) => {
        const paged = warmLayerWithColdPageAtLeast(prev, warmLayerSessionKey, neededColdPage);
        return warmLayerWithExpandedTurn(paged, warmLayerSessionKey, question.turn, true);
      });
    }
    jumpToQuestion(question);
  }, [turnGroups.length, warmLayerSessionKey]);

  // ── Hot zone: fully rendered from hotStartIdx to end ─────────────────────
  // Memoized separately from the assembly so streaming tokens don't rebuild
  // the warm/cold zone JSX trees. Uses LiveStreamContext for streaming data
  // (added by upstream PR #3423) instead of per-call renderSegments.
  const empty = items.length === 0;

  useLayoutEffect(() => {
    if (!empty) return;
    const el = scrollRef.current;
    if (!el) return;
    el.scrollTop = 0;
    stick.current = false;
    const frame = requestAnimationFrame(() => {
      el.scrollTop = 0;
    });
    return () => cancelAnimationFrame(frame);
  }, [empty, scrollRef, stick, tabId]);

  // The hot-zone memo must not depend on the live stream's full text/reasoning
  // — that would rebuild the whole element array on every streaming token
  // (LiveAssistantMessage reads those via LiveStreamContext instead). The memo
  // only needs presence flags, which flip at most once per turn.
  const liveId = live?.id;
  const liveHasAnswerText = Boolean(live?.text.trim());
  const liveHasReasoning = Boolean(live?.reasoning);

  const hotZoneNodes = useMemo<ReactNode[]>(() => {
    const out: ReactNode[] = [];
    const pushTurnActions = (turn: number | undefined, turnItems: readonly Item[]) => {
      if (turn == null) return;
      let actionText = "";
      for (const item of turnItems) {
        if (item.kind !== "assistant" || item.streaming || !item.text.trim()) continue;
        actionText = appendTurnActionCopyText(actionText, item.text);
      }
      if (!actionText.trim()) return;
      const openMenu = openAction && openAction.turn === turn ? openAction.menu : null;
      out.push(
        <TurnActions
          key={`ta-${turn}`}
          text={actionText}
          turn={turn}
          openMenu={openMenu}
          onOpenMenu={(menu) => setOpenAction(menu ? { turn, menu } : null)}
          checkpoint={checkpointsByTurn.get(turn)}
          actionPending={actionPending}
          rewindDisabled={rewindDisabled}
          hoverMenus={actionHoverMenus}
          isLastTurn={turn === lastTurn}
          onRewind={(targetTurn, scope) => {
            onRewind?.(targetTurn, scope);
            setOpenAction(null);
          }}
        />,
      );
    };

    const pushTurnBody = (key: string, turnItems: readonly Item[], turnIsActive: boolean) => {
      const segments = partitionTurnItems(turnItems, liveId, liveHasAnswerText, liveHasReasoning);
      const turnHasOutsideContent = segments.some((segment) => segment.outsideItems.length > 0);
      segments.forEach((segment, segmentIndex) => {
        const isLastSegment = segmentIndex === segments.length - 1;
        if (segment.processItems.length > 0) {
          out.push(
            <TurnCollapse
              key={`turn-process-${key}-${segment.processItems[0].id}`}
              items={segment.processItems}
              durationMs={isLastSegment ? turnWorkDurationMs(turnItems) : 0}
              mode={displayMode}
              subcalls={subcallsByParent}
              tabId={tabId}
              creationMode={creationMode}
              turnStartAt={turnIsActive && isLastSegment ? turnStartAt : undefined}
              turnActive={turnIsActive && isLastSegment}
              preferredKind="reasoning"
              labelStyle={isLastSegment ? "full" : "counts"}
              hasOutsideContent={turnHasOutsideContent}
            />,
          );
        }
        for (const item of segment.outsideItems) {
          if (item.kind === "notice") {
            if (isSteerNoticeText(item.text)) {
              out.push(<SteerCard key={item.id} text={item.text} />);
              continue;
            }
            out.push(
              <NoticeCard
                key={item.id}
                item={item}
                actionDisabled={running}
                onAction={item.action === "continue_delivery" ? () => onPrompt(t("notice.deliveryIncompleteContinuePrompt")) : undefined}
              />,
            );
          } else {
            out.push(
              <LiveAssistantMessage
                key={item.id}
                item={assistantAnswerOnly(item)}
                defaultExpanded={false}
                expandWhileStreaming={false}
                truncateStreamingReasoning={true}
                creationMode={creationMode}
                reasoningDisplay="hide"
              />,
            );
          }
        }
      });
    };

    const hotGroups = turnGroups.filter((group) => group.startIdx >= hotStartIdx);
    const firstHotStart = hotGroups[0]?.startIdx ?? items.length;
    if (hotStartIdx < firstHotStart) {
      pushTurnBody("prelude", items.slice(hotStartIdx, firstHotStart), false);
    }

    for (let index = 0; index < hotGroups.length; index++) {
      const group = hotGroups[index];
      const user = group.userItem;
      if (user.kind !== "user") continue;
      const turn = userTurn.get(user.id);
      const checkpoint = turn == null ? undefined : checkpointsByTurn.get(turn);
      const turnItems = items.slice(group.startIdx + 1, group.endIdx);
      const turnIsActive = running && index === hotGroups.length - 1;
      out.push(
        <UserMessage
          key={user.id}
          id={user.id}
          text={user.text}
          submitText={user.submitText}
          failed={user.failed}
          createdAt={user.createdAt}
          turn={turn}
          anchorId={questionAnchorId(user.id)}
          onEdit={onEditPrompt}
          editDisabled={rewindDisabled || !checkpoint?.canConversation}
        />,
      );
      pushTurnBody(user.id, turnItems, turnIsActive);
      if (!turnIsActive) pushTurnActions(turn, turnItems);
    }
    return out;
  }, [hotStartIdx, items, openAction, actionPending, rewindDisabled, running, onEditPrompt, onPrompt, onRewind, subcallsByParent, userTurn, checkpointsByTurn, displayMode, turnGroups, tabId, actionHoverMenus, creationMode, lastTurn, turnStartAt, liveId, liveHasAnswerText, liveHasReasoning, t]);

  // ── Assemble rendered output ──────────────────────────────────────────────
  // Warm/cold zone is a separate memo'd WarmZone component so streaming tokens
  // don't rebuild it. The hot zone uses LiveAssistantMessage (reads live from
  // LiveStreamContext) so streaming updates are captured immediately.
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
      >
        {empty && !hydrating && <Welcome onPrompt={onPrompt} variant={welcomeVariant} />}

        <LiveStreamContext.Provider value={live}>
          {hasOlderHistory && (
            <button
              type="button"
              className="warm-collapse"
              onClick={onLoadOlderHistory}
              disabled={loadingOlderHistory}
            >
              {loadingOlderHistory ? t("common.loading") : t("transcript.showEarlierHistory", { n: olderHistoryCount })}
            </button>
          )}
          {turnGroups.length > HOT_TURNS && (
            <WarmZone
              turnGroups={turnGroups}
              expandedWarmTurns={expandedWarmTurns}
              warmStartTurn={warmStartTurn}
              warmEndTurn={warmEndTurn}
              coldTurnCount={coldTurnCount}
              scrollRef={scrollRef}
              warmItems={items}
              warmSubcalls={subcallsByParent}
              warmUserTurn={userTurn}
              warmCheckpoints={checkpointsByTurn}
              warmLastTurn={lastTurn}
              warmDisplayMode={displayMode}
              warmOpenAction={openAction}
              warmActionPending={actionPending}
              warmRewindDisabled={rewindDisabled}
              warmActionHoverMenus={actionHoverMenus}
              warmOnRewind={onRewind}
              warmSetOpenAction={setOpenAction}
              warmOnEdit={onEditPrompt}
              warmOnPrompt={onPrompt}
              warmRunning={running}
              tabId={tabId}
              creationMode={creationMode}
              onToggleColdPage={() => setWarmLayerState((prev) => warmLayerWithNextColdPage(prev, warmLayerSessionKey))}
              onToggleWarmTurn={(g, expand) => {
                setWarmLayerState((prev) => warmLayerWithExpandedTurn(prev, warmLayerSessionKey, g, expand));
              }}
            />
          )}
          <div ref={entranceRef}>
            {hotZoneNodes}
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

// ── WarmZone sub-component (React.memo for streaming isolation) ────────────
// Receives structural props only; reads streaming state (items, live) via refs
// so it never invalidates on streaming token arrival.

const WarmZone = memo(function WarmZone({
  turnGroups,
  expandedWarmTurns,
  warmStartTurn,
  warmEndTurn,
  coldTurnCount,
  scrollRef,
  warmItems,
  warmSubcalls,
  warmUserTurn,
  warmCheckpoints,
  warmLastTurn,
  warmDisplayMode,
  warmOpenAction,
  warmActionPending,
  warmRewindDisabled,
  warmActionHoverMenus,
  warmOnRewind,
  warmSetOpenAction,
  warmOnEdit,
  warmOnPrompt,
  warmRunning,
  tabId,
  creationMode,
  onToggleColdPage,
  onToggleWarmTurn,
}: {
  turnGroups: TurnGroup[];
  expandedWarmTurns: ReadonlySet<number>;
  warmStartTurn: number;
  warmEndTurn: number;
  coldTurnCount: number;
  scrollRef: React.RefObject<HTMLDivElement | null>;
  warmItems: readonly Item[];
  warmSubcalls: ReadonlyMap<string, ToolItem[]>;
  warmUserTurn: ReadonlyMap<string, number>;
  warmCheckpoints: ReadonlyMap<number, CheckpointMeta>;
  warmLastTurn?: number;
  warmDisplayMode: DisplayMode;
  warmOpenAction: OpenTurnAction | null;
  warmActionPending: boolean;
  warmRewindDisabled: boolean;
  warmActionHoverMenus: boolean;
  warmOnRewind: ((turn: number, scope: string) => void) | undefined;
  warmSetOpenAction: (action: OpenTurnAction | null) => void;
  warmOnEdit?: (turn: number, displayText: string, submitText?: string) => boolean | void | Promise<boolean | void>;
  warmOnPrompt: (text: string) => void;
  warmRunning: boolean;
  tabId?: string;
  creationMode?: boolean;
  onToggleColdPage: () => void;
  onToggleWarmTurn: (g: number, expand: boolean) => void;
}) {
  const t = useT();
  const out: React.ReactNode[] = [];

  // 1. Cold zone: paginated warm turns (show more button).
  if (coldTurnCount > 0) {
    out.push(
      <button
        key="cold-load-more"
        type="button"
        className="warm-collapse"
        onClick={onToggleColdPage}
      >
        {t("transcript.showEarlierHistory", { n: coldTurnCount })}
      </button>,
    );
  }

  // 2. Warm zone: collapsed/expanded warm turn cards.
  if (turnGroups.length > HOT_TURNS) {
    for (let g = warmStartTurn; g < warmEndTurn; g++) {
      const group = turnGroups[g];
      if (!group) continue;
      const expanded = expandedWarmTurns.has(g);

      if (expanded) {
        const userText = group.userItem.kind === "user" ? group.userItem.text : "";
        out.push(
          <WarmTurnCard
            key={`warm-${g}`}
            userText={warmUserPreview(userText)}
            assistantPreview={group.assistantPreview}
            toolCount={group.toolCount}
            expanded={true}
            onToggle={() => onToggleWarmTurn(g, false)}
          >
            {/* Expanded warm turns render items that are stable (never the
                streaming turn), so this captures items/live via a ref. */}
            <WarmTurnItems
              startIdx={group.startIdx}
              endIdx={group.endIdx}
              items={warmItems}
              subcalls={warmSubcalls}
              userTurnMap={warmUserTurn}
              checkpoints={warmCheckpoints}
              openAction={warmOpenAction}
              actionPending={warmActionPending}
              rewindDisabled={warmRewindDisabled}
              actionHoverMenus={warmActionHoverMenus}
              onRewind={warmOnRewind}
              setOpenAction={warmSetOpenAction}
              onEdit={warmOnEdit}
              onPrompt={warmOnPrompt}
              running={warmRunning}
              tabId={tabId}
              creationMode={creationMode}
              lastTurn={warmLastTurn}
              mode={warmDisplayMode}
            />
          </WarmTurnCard>,
        );
      } else {
        const userText = group.userItem.kind === "user" ? group.userItem.text : "";
        out.push(
          <WarmTurnCard
            key={`warm-${g}`}
            userText={warmUserPreview(userText)}
            assistantPreview={group.assistantPreview}
            toolCount={group.toolCount}
            expanded={false}
            onToggle={() => {
              onToggleWarmTurn(g, true);
              const el = scrollRef.current;
              const node = document.getElementById(questionAnchorId(group.userItem.id));
              if (el && node) {
                requestAnimationFrame(() => {
                  el.scrollTo({ top: node.offsetTop - el.offsetTop - 80, behavior: "smooth" });
                });
              }
            }}
          />,
        );
      }
    }
  }

  return out;
});

function WarmTurnItems({
  startIdx,
  endIdx,
  items,
  subcalls,
  userTurnMap,
  checkpoints,
  openAction,
  actionPending,
  rewindDisabled,
  actionHoverMenus,
  onRewind,
  setOpenAction,
  onEdit,
  onPrompt,
  running,
  tabId,
  creationMode = false,
  lastTurn,
  mode,
}: {
  startIdx: number;
  endIdx: number;
  items: readonly Item[];
  subcalls: ReadonlyMap<string, ToolItem[]>;
  userTurnMap: ReadonlyMap<string, number>;
  checkpoints: ReadonlyMap<number, CheckpointMeta>;
  openAction: OpenTurnAction | null;
  actionPending: boolean;
  rewindDisabled: boolean;
  actionHoverMenus: boolean;
  onRewind: ((turn: number, scope: string) => void) | undefined;
  setOpenAction: (action: OpenTurnAction | null) => void;
  onEdit?: (turn: number, displayText: string, submitText?: string) => boolean | void | Promise<boolean | void>;
  onPrompt: (text: string) => void;
  running: boolean;
  tabId?: string;
  creationMode?: boolean;
  lastTurn?: number;
  mode: DisplayMode;
}) {
  const t = useT();
  const nodes: React.ReactNode[] = [];
  const user = items[startIdx];
  if (!user || user.kind !== "user") return nodes;

  const turn = userTurnMap.get(user.id);
  const checkpoint = turn == null ? undefined : checkpoints.get(turn);
  const turnItems = items.slice(startIdx + 1, Math.min(endIdx, items.length));
  const segments = partitionTurnItems(turnItems);
  const turnHasOutsideContent = segments.some((segment) => segment.outsideItems.length > 0);
  nodes.push(
    <UserMessage
      key={user.id}
      id={user.id}
      text={user.text}
      submitText={user.submitText}
      failed={user.failed}
      createdAt={user.createdAt}
      turn={turn}
      anchorId={questionAnchorId(user.id)}
      onEdit={onEdit}
      editDisabled={rewindDisabled || !checkpoint?.canConversation}
    />,
  );
  segments.forEach((segment, segmentIndex) => {
    const isLastSegment = segmentIndex === segments.length - 1;
    if (segment.processItems.length > 0) {
      nodes.push(
        <TurnCollapse
          key={`warm-process-${user.id}-${segment.processItems[0].id}`}
          items={segment.processItems}
          durationMs={isLastSegment ? turnWorkDurationMs(turnItems) : 0}
          mode={mode}
          subcalls={subcalls}
          tabId={tabId}
          creationMode={creationMode}
          preferredKind="reasoning"
          labelStyle={isLastSegment ? "full" : "counts"}
          hasOutsideContent={turnHasOutsideContent}
        />,
      );
    }
    for (const item of segment.outsideItems) {
      if (item.kind === "notice") {
        if (isSteerNoticeText(item.text)) {
          nodes.push(<SteerCard key={item.id} text={item.text} />);
          continue;
        }
        nodes.push(
          <NoticeCard
            key={item.id}
            item={item}
            actionDisabled={running}
            onAction={item.action === "continue_delivery" ? () => onPrompt(t("notice.deliveryIncompleteContinuePrompt")) : undefined}
          />,
        );
      } else {
        nodes.push(
          <AssistantMessage
            key={item.id}
            item={assistantAnswerOnly(item)}
            defaultExpanded={false}
            creationMode={creationMode}
          />,
        );
      }
    }
  });

  let actionText = "";
  for (const item of turnItems) {
    if (item.kind !== "assistant" || item.streaming || !item.text.trim()) continue;
    actionText = appendTurnActionCopyText(actionText, item.text);
  }
  if (turn != null && actionText.trim()) {
    const openMenu = openAction && openAction.turn === turn ? openAction.menu : null;
    nodes.push(
      <TurnActions
        key={`ta-${turn}`}
        text={actionText}
        turn={turn}
        openMenu={openMenu}
        onOpenMenu={(menu) => setOpenAction(menu ? { turn, menu } : null)}
        checkpoint={checkpoints.get(turn)}
        actionPending={actionPending}
        rewindDisabled={rewindDisabled}
        hoverMenus={actionHoverMenus}
        isLastTurn={turn === lastTurn}
        onRewind={(targetTurn, scope) => {
          onRewind?.(targetTurn, scope);
          setOpenAction(null);
        }}
      />,
    );
  }
  return nodes;
}

// ── Warm turn summary card ────────────────────────────────────────────────────

function WarmTurnCard({
  userText,
  assistantPreview,
  toolCount,
  expanded,
  onToggle,
  children,
}: {
  userText: string;
  assistantPreview: string;
  toolCount: number;
  expanded: boolean;
  onToggle: () => void;
  children?: React.ReactNode;
}) {
  const t = useT();
  const contentRef = useRef<HTMLDivElement>(null);
  const prevHeightRef = useRef(0);
  useGSAPCollapse(contentRef, expanded, { prevHeight: prevHeightRef.current });
  // Always render both children so the container's scrollHeight reflects
  // the correct content at all times.  The inactive one is display:none.
  return (
    <div className={`warm-turn${expanded ? " warm-turn--expanded" : ""}`}>
      <button
        type="button"
        className="warm-turn__head"
        onClick={() => {
          // Capture height before DOM swap so the collapse animation
          // starts from the correct (expanded) height.
          const el = contentRef.current;
          if (el) {
            el.style.height = "auto";
            prevHeightRef.current = el.scrollHeight;
          }
          onToggle();
        }}
        aria-expanded={expanded}
      >
        <span className="warm-turn__chevron">
          <ChevronRight className={expanded ? "warm-turn__chevron--open" : ""} size={13} />
        </span>
        <span className="warm-turn__preview">{userText}</span>
        <span className="warm-turn__meta">
          {toolCount > 0 && <span>{t("transcript.toolCount", { n: toolCount })}</span>}
        </span>
      </button>
      <div ref={contentRef} className="warm-turn__content">
        <div className="warm-turn__body" style={{ display: expanded ? undefined : "none" }}>{children}</div>
        {assistantPreview && (
          <div className="warm-turn__assistant" style={{ display: expanded ? "none" : undefined }}>{assistantPreview}</div>
        )}
      </div>
    </div>
  );
}

// ── TurnCollapse: one process fold per user turn ─────────────────────────────

type TurnCollapseProps = {
  items: Item[];
  durationMs: number;
  mode: DisplayMode;
  subcalls: ReadonlyMap<string, ToolItem[]>;
  tabId?: string;
  creationMode?: boolean;
  turnStartAt?: number;
  turnActive?: boolean;
  preferredKind?: "tool" | "reasoning" | "process";
  // "full" carries the turn's work-duration label; "counts" is for earlier
  // segments of a multi-fold turn, which only list what they contain — the
  // turn's wall-clock belongs to the segment where the turn ends.
  labelStyle?: "full" | "counts";
  // Whether the turn renders anything outside this fold (answer text, warning,
  // steer). When nothing is outside, the fold is the turn's only content and
  // must not collapse it away.
  hasOutsideContent?: boolean;
};

function TurnCollapse({ items, durationMs, mode, subcalls, tabId, creationMode = false, turnStartAt, turnActive = false, preferredKind, labelStyle = "full", hasOutsideContent = true }: TurnCollapseProps) {
  const t = useT();
  const live = useContext(LiveStreamContext);
  const [foldPreference, setFoldPreference] = useState<ProcessFoldPreference>(getProcessFoldPreference);
  const [open, setOpen] = useState(() => getProcessFoldPreference() === "expanded" || !hasOutsideContent);
  const userOverriddenOpen = useRef(false);
  const prevRunningRef = useRef(false);
  const bodyRef = useRef<HTMLDivElement>(null);
  useEffect(() => onProcessFoldPreferenceChange(setFoldPreference), []);

  // Keep only items the body will actually render — an expandable fold over
  // nothing is worse than no fold. Assistant items reach the fold stripped to
  // their reasoning (answer text renders outside), so reasoning presence is
  // the only thing that keeps them.
  const displayItems = useMemo(() => {
    return items.filter((it) => {
      if (it.kind === "assistant") {
        return Boolean(it.reasoning || (live?.id === it.id && live.reasoning));
      }
      if (it.kind === "phase") return true;
      if (it.kind === "notice") return true;
      if (it.kind === "compaction") return true;
      if (it.kind !== "tool") return false;
      if (it.parentId || it.name === "todo_write" || it.name === "exit_plan_mode") return false;
      return true;
    });
  }, [items, mode, live?.id, live?.reasoning]);

  const seconds = Math.round(durationMs / 1000);

  const hasRunningProcess = displayItems.some((it) => {
    if (it.kind === "tool") return it.status === "running";
    if (it.kind !== "assistant") return false;
    if (live?.id === it.id) return !live.reasoningComplete;
    return it.streaming && !it.reasoningComplete;
  });
  const hasLiveAssistant = displayItems.some((it) => it.kind === "assistant" && live?.id === it.id);
  const hasRunningWork = turnActive || hasRunningProcess || hasLiveAssistant;
  const now = useTick(hasRunningWork);
  const runningDurationMs = hasRunningWork
    ? turnStartAt
      ? Math.max(0, now - turnStartAt)
      : live?.reasoningStartedAt
        ? Math.max(0, now - live.reasoningStartedAt)
        : 0
    : 0;
  const effectiveDurationMs = hasRunningWork ? Math.max(durationMs, runningDurationMs) : durationMs;

  useGSAPCollapse(bodyRef, open);
  useEffect(() => {
    const wasRunning = prevRunningRef.current;
    prevRunningRef.current = hasRunningWork;
    if (hasRunningWork) {
      if (!wasRunning) userOverriddenOpen.current = false;
      if (!userOverriddenOpen.current) setOpen(true);
    } else if (wasRunning && !userOverriddenOpen.current && hasOutsideContent && foldPreference !== "expanded") {
      setOpen(false);
    }
  }, [hasRunningWork, hasOutsideContent, foldPreference]);
  // Switching the preference is an explicit act that also applies to folds
  // already on screen, not only future ones; it clears per-fold manual
  // overrides so the whole transcript lands in one consistent state.
  const prevFoldPreference = useRef(foldPreference);
  useEffect(() => {
    if (prevFoldPreference.current === foldPreference) return;
    prevFoldPreference.current = foldPreference;
    userOverriddenOpen.current = false;
    if (foldPreference === "expanded") {
      setOpen(true);
    } else if (!hasRunningWork && hasOutsideContent) {
      setOpen(false);
    }
  }, [foldPreference, hasRunningWork, hasOutsideContent]);

  if (displayItems.length === 0) return null;

  const collapseKind = preferredKind ?? (displayItems.some((it) => it.kind === "tool")
    ? "tool"
    : displayItems.some((it) => it.kind === "assistant" && Boolean(it.reasoning))
      ? "reasoning"
      : "process");
  const baseLabel = collapseKind === "reasoning"
    ? workStatusLabel(effectiveDurationMs, hasRunningWork, t)
    : seconds > 0
      ? t("transcript.processedDuration", { s: seconds })
      : t("transcript.processed");
  // Surface what the closed fold hides — a bare duration reads as pure timing
  // and users have no way to know process detail sits behind it.
  const toolCount = displayItems.reduce((n, it) => n + (it.kind === "tool" ? 1 : 0), 0);
  const thoughtCount = displayItems.reduce((n, it) => n + (it.kind === "assistant" ? 1 : 0), 0);
  const countParts: string[] = [];
  if (toolCount > 0) countParts.push(t("transcript.toolCount", { n: toolCount }));
  if (thoughtCount > 0) countParts.push(t("transcript.thoughtCount", { n: thoughtCount }));
  const label = labelStyle === "counts"
    ? (countParts.length > 0 ? countParts.join(" · ") : t("transcript.processed"))
    : countParts.length > 0
      ? `${baseLabel} · ${countParts.join(" · ")}`
      : baseLabel;
  const creationLabel = collapseKind === "tool"
    ? t("creation.toolCallsLabel")
    : collapseKind === "reasoning"
      ? label
      : label;

  // Pre-compute body: group consecutive completed read-only tools into ReadOnlyBatch
  const body: ReactNode[] = [];
  const roBatch: ToolItem[] = [];
  const toolBatch: ToolItem[] = [];
  let toolBatchKind: ToolGroupKind | null = null;
  const flushRO = () => {
    if (roBatch.length === 0) return;
    body.push(<ReadOnlyBatch key={`rob-${roBatch[0].id}`} items={[...roBatch]} subcalls={subcalls} tabId={tabId} />);
    roBatch.length = 0;
  };
  const flushToolBatch = () => {
    if (!toolBatchKind || toolBatch.length === 0) return;
    body.push(<ToolGroup key={`tg-${toolBatch[0].id}`} kind={toolBatchKind} items={[...toolBatch]} subcalls={subcalls} tabId={tabId} />);
    toolBatch.length = 0;
    toolBatchKind = null;
  };
  for (const it of displayItems) {
    if (creationMode && it.kind === "tool" && isCreationGroupableTool(it as ToolItem)) {
      const kind = toolGroupKind(it as ToolItem);
      if (kind) {
        if (toolBatchKind && toolBatchKind !== kind) flushToolBatch();
        toolBatchKind = kind;
        toolBatch.push(it as ToolItem);
        continue;
      }
    }
    if (it.kind !== "tool") {
      flushToolBatch();
      flushRO();
    }
    if (!creationMode && it.kind === "tool" && !it.parentId && it.name !== "todo_write" && it.name !== "exit_plan_mode" && it.status !== "running" && isReadOnlyTool(it.name)) {
      roBatch.push(it as ToolItem);
      continue;
    }
    if (it.kind === "tool") {
      flushToolBatch();
      flushRO();
    }
    switch (it.kind) {
      case "tool":
        if (it.parentId) break;
        if (it.name === "todo_write") break;
        if (it.name === "exit_plan_mode") break;
        body.push(<ToolCard key={it.id} item={it as ToolItem} subcalls={subcalls.get(it.id)} tabId={tabId} />);
        break;
      case "phase": body.push(<PhaseCard key={it.id} text={it.text} />); break;
      case "notice": body.push(<NoticeCard key={it.id} item={it} />); break;
      case "compaction": body.push(<CompactionCard key={it.id} item={it} />); break;
      case "assistant":
        // Answer text renders outside the fold (partitionTurnItems strips it),
        // so the fold only ever shows the reasoning segment.
        body.push(<InlineAssistantReasoning key={`${it.id}-reasoning`} item={it as AssistantItem} />);
        break;
    }
  }
  flushToolBatch();
  flushRO();

  return (
    <div className={`turn-collapse${open ? " turn-collapse--open" : ""}`} data-kind={collapseKind} data-entrance={displayItems[0]?.id || undefined}>
      <button
        type="button"
        className="reasoning__head"
        onClick={() => {
          userOverriddenOpen.current = true;
          setOpen((v) => !v);
        }}
        aria-expanded={open}
      >
        <span className="turn-collapse__label" data-creation-label={creationLabel}>{label}</span>
        {!hasRunningWork && <ChevronRight className={`reasoning__chevron${open ? " reasoning__chevron--open" : ""}`} size={12} />}
      </button>
      <div ref={bodyRef} className="turn-collapse__body">{body}</div>
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

function PhaseCard({ text }: { text: string }) {
  return <div className="phase" data-entrance="true"><ProcessPhaseIcon size={12} /><span>{text}</span></div>;
}

// A mid-turn steer is the user's own message, so it renders on the user side
// of the transcript instead of disappearing into the work fold.
function SteerCard({ text }: { text: string }) {
  const t = useT();
  const body = text.startsWith(STEER_NOTICE_PREFIX) ? text.slice(STEER_NOTICE_PREFIX.length) : text;
  return (
    <div className="steer-line" data-entrance="true">
      <div className="steer-line__bubble" title={t("transcript.steer")}>
        <span className="steer-line__icon" aria-hidden="true">↪</span>
        <span className="steer-line__text">{body}</span>
      </div>
    </div>
  );
}

export function NoticeCard({ item, onAction, actionDisabled = false }: { item: NoticeItem; onAction?: () => void; actionDisabled?: boolean }) {
  const t = useT();
  const StatusIcon = item.level === "warn" ? TriangleAlert : Info;
  return (
    <div className={`notice-line notice-line--${item.level}${item.variant ? ` notice-line--${item.variant}` : ""}`} data-entrance="true">
      <StatusIcon className="notice-line__icon" size={14} aria-hidden="true" />
      <div className="notice-line__text">
        {item.title ? <div className="notice-line__title">{item.title}</div> : null}
        <div className="notice-line__body">{item.text}</div>
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
