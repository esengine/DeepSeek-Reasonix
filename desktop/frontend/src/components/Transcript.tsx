import { forwardRef, memo, type CSSProperties, type MouseEvent as ReactMouseEvent, type ReactNode, useCallback, useContext, useEffect, useMemo, useRef, useState, useSyncExternalStore } from "react";
import { Virtuoso, type Components, type ItemProps, type ListItem, type ListProps } from "react-virtuoso";
import type { ControllerLiveStore, Item, LiveStream } from "../lib/useController";
import type { CheckpointMeta } from "../lib/types";
import type { InvocationMetadataMap } from "../lib/invocationDisplay";
import { useT } from "../lib/i18n";
import { AssistantMessage, InvocationMetadataContext, TurnActions, UserMessage } from "./Message";
import { ProcessCompactIcon, ProcessPhaseIcon } from "./ProcessCard";
import { ToolCard } from "./ToolCard";
import { ExtensionCard } from "./ExtensionCard";
import { ArrowDown, ChevronRight, CirclePlay, FileSearch, Info, TriangleAlert } from "lucide-react";
import { Welcome } from "./Welcome";
import { ReadOnlyBatch } from "./ReadOnlyBatch";
import { ToolGroup } from "./ToolGroup";
import { getProcessFoldPreference, onProcessFoldPreferenceChange, type ProcessFoldPreference } from "../lib/processFoldPreference";
import { STEER_NOTICE_PREFIX, isSteerNoticeText } from "../lib/useController";
import { useTranscriptEntranceAnimation } from "../lib/useEntranceAnimation";
import { useTranscriptSelectionRetention } from "../lib/useTranscriptSelectionRetention";
import { compactQuestionText, lastQuestionTurn, questionAnchorId, questionTurnsById, scrollVersion, type QuestionAnchor } from "../lib/transcriptGrouping";
import {
  buildTranscriptRows,
  buildTurnModels,
  foldMapWithReasoningOpen,
  foldMapWithToggle,
  foldSegmentStates,
  historyEntryIdForRow,
  reconcileFoldEntries,
  estimateTranscriptRowSize,
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
import { getTranscriptStore } from "../lib/transcriptStore";
import { acquireMarkdownWorkerClient, releaseMarkdownWorkerClient } from "../lib/markdownWorkerClient";
import { noteTranscriptRowCounts } from "../lib/sessionDiagnostics";
import { useReasoningDisplayMode } from "../lib/reasoningDisplayPreference";
import { InlineAssistantReasoning } from "./InlineAssistantReasoning";
import { LiveStreamContext } from "./LiveStreamContext";
import { useTranscriptSelectableRows } from "../lib/useTranscriptSelectableRows";
import { TranscriptSelectionOverlay } from "./TranscriptSelectionOverlay";
import { useCreationTranscriptScrollbar } from "../lib/useCreationTranscriptScrollbar";
import { useTranscriptScrollInteractions } from "../lib/useTranscriptScrollInteractions";
import { TRANSCRIPT_AT_BOTTOM_THRESHOLD_PX, useTranscriptVirtuosoScroll } from "../lib/useTranscriptVirtuosoScroll";
import { useTranscriptVirtuosoFirstItemIndex } from "../lib/transcriptVirtuosoIndex";
type OpenTurnAction = { turn: number; menu: "summary" | "rewind" };
const QUESTION_NAV_MIN_COUNT = 2;
type AssistantReasoningDisplay = "normal" | "hide";
const EMPTY_CHECKPOINTS: CheckpointMeta[] = [];
const EMPTY_INVOCATION_METADATA: InvocationMetadataMap = {};

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

type TranscriptVirtuosoContext = {
  tabId?: string;
  scrollElement: HTMLDivElement | null;
  nativeScrollbarDragging: boolean;
  overlayRevision: string;
  olderHistory: null | {
    loading: boolean;
    label: string;
    onLoad?: () => void;
  };
};

const TranscriptVirtuosoItem = forwardRef<HTMLDivElement, ItemProps<TranscriptRow> & { context: TranscriptVirtuosoContext }>(
  function TranscriptVirtuosoItem({ item, context, children, style, ...props }, ref) {
    const entryId = historyEntryIdForRow(item);
    useEffect(() => {
      if (entryId) getTranscriptStore().requestEntryFullContent(context.tabId, entryId);
    }, [context.tabId, entryId]);
    const knownSize = Number.parseFloat(String(props["data-known-size"] ?? ""));
    const frozenStyle = context.nativeScrollbarDragging && Number.isFinite(knownSize) && knownSize > 0
      ? { ...style, boxSizing: "border-box" as const, height: knownSize, overflow: "hidden" as const }
      : style;
    return (
      <div {...props} ref={ref} style={frozenStyle} data-row-key={String(item.key)} className="transcript__row">
        {children}
      </div>
    );
  },
);

const TranscriptVirtuosoList = forwardRef<HTMLDivElement, ListProps & { context: TranscriptVirtuosoContext }>(
  function TranscriptVirtuosoList({ context, children, ...props }, ref) {
    return (
      <div {...props} ref={ref} className="transcript__virtual-sizer">
        <TranscriptSelectionOverlay
          tabId={context.tabId ?? ""}
          scrollElement={context.scrollElement}
          virtualRevision={context.overlayRevision}
        />
        {children}
      </div>
    );
  },
);

function TranscriptVirtuosoHeader({ context }: { context: TranscriptVirtuosoContext }) {
  if (!context.olderHistory) return null;
  return (
    <div className="transcript__header">
      <button
        type="button"
        className="warm-collapse transcript__older"
        onClick={context.olderHistory.onLoad}
        disabled={context.olderHistory.loading}
      >
        {context.olderHistory.label}
      </button>
    </div>
  );
}

const TRANSCRIPT_VIRTUOSO_COMPONENTS: Components<TranscriptRow, TranscriptVirtuosoContext> = {
  Item: TranscriptVirtuosoItem,
  List: TranscriptVirtuosoList,
};

const TRANSCRIPT_VIRTUOSO_COMPONENTS_WITH_HEADER: Components<TranscriptRow, TranscriptVirtuosoContext> = {
  ...TRANSCRIPT_VIRTUOSO_COMPONENTS,
  Header: TranscriptVirtuosoHeader,
};

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
  onOpenChanges,
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
  olderHistoryCount = 0,
  loadingOlderHistory = false,
  onLoadOlderHistory,
  turnStartAt,
  invocationMetadata = EMPTY_INVOCATION_METADATA,
}: {
  items: Item[];
  live?: LiveStream;
  liveStore?: ControllerLiveStore;
  tabId?: string;
  footerHeight?: number;
  onPrompt: (text: string) => void;
  onDeliveryContinue?: () => void;
  onOpenChanges?: () => void;
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
    virtuosoRef,
    scrollRef,
    itemSize,
    nativeScrollbarDragging,
    scrollElement,
    pinnedRef: stick,
    onWheelIntent,
    onPointerDownIntent,
    onNestedScrollIntent,
    onTouchStartIntent,
    onTouchMoveIntent,
    onKeyScrollIntent,
    isAtBottom,
    scrollerRef,
    atBottomStateChange,
    scrollToBottom,
    followGrowingTail,
    scrollToDataIndex,
    releaseTailFollow,
    setMode: setScrollMode,
    writeOffset,
    reset: resetScroll,
    finishProgrammaticScroll,
  } = useTranscriptVirtuosoScroll();
  const autoScrollFrame = useRef<number | null>(null);
  const virtuosoReadyRef = useRef(false);

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

  // A new local question is an explicit request to reveal the tail. Prepending
  // older history keeps the same last id and is left entirely to Virtuoso's
  // firstItemIndex anchor contract.
  const questionTailRef = useRef({ length: 0, lastId: "" });
  useEffect(() => {
    const lastId = questions[questions.length - 1]?.id ?? "";
    const prev = questionTailRef.current;
    questionTailRef.current = { length: questions.length, lastId };
    if (prev.length > 0 && questions.length > prev.length && lastId !== prev.lastId) scrollToBottom();
  }, [questions, scrollToBottom]);

  // Reset the auto-scroll pin when switching tabs so the new session always
  // starts at the bottom. Without this, stick.current from the previous tab
  // persists across React re-renders (Transcript is not keyed by tabId) and
  // disables auto-scroll when the user had scrolled up in the old tab (#4584).
  useEffect(() => {
    resetScroll();
    virtuosoReadyRef.current = false;
  }, [resetScroll, revealSignal, tabId]);

  // Auto-scroll to bottom during streaming. Coalesce fast token/reasoning
  // updates into one layout read/write per animation frame.
  const contentVersion = useMemo(() => scrollVersion(items), [items]);
  useEffect(() => {
    if (items.length === 0) return;
    if (!virtuosoReadyRef.current) return;
    if (!stick.current) return;
    if (autoScrollFrame.current !== null) return;
    autoScrollFrame.current = requestAnimationFrame(() => {
      autoScrollFrame.current = null;
      if (!stick.current) return;
      followGrowingTail();
    });
  }, [contentVersion, followGrowingTail, live?.text?.length ?? 0, live?.reasoning?.length ?? 0, stick]);
  useEffect(() => {
    return () => {
      if (autoScrollFrame.current !== null) {
        cancelAnimationFrame(autoScrollFrame.current);
        autoScrollFrame.current = null;
      }
    };
  }, []);

  // Footer chrome resize only. Item growth stays on followGrowingTail.
  useEffect(() => {
    if (!virtuosoReadyRef.current || !stick.current) return;
    scrollToBottom();
  }, [footerHeight, scrollToBottom, stick]);

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
  // Keep the load-older affordance in Virtuoso's measured Header slot so an
  // older page is a true data prepend, rather than an insertion after row 0.
  const virtualRows = useMemo(
    () => rows[0]?.kind === "older-history" ? rows.slice(1) : rows,
    [rows],
  );
  const rowIndexByKey = useMemo(() => {
    const map = new Map<string, number>();
    virtualRows.forEach((row, index) => map.set(String(row.key), index));
    return map;
  }, [virtualRows]);
  const [selectableRows, liveSelectableRows] = useTranscriptSelectableRows(virtualRows, live);
  const selectionRetention = useTranscriptSelectionRetention({
    tabId,
    revealSignal,
    rowIndexByKey,
    selectableRows,
    selectableRowOverrides: liveSelectableRows,
    scrollRef,
    setScrollMode,
    writeOffset,
    cancelStreamingScroll: cancelStreamingAndFollow,
  });
  const scrollInteractions = useTranscriptScrollInteractions({
    scrollRef,
    cancelStreamingScroll: cancelStreamingAutoScroll,
    onWheelIntent,
    onTouchMoveIntent,
    onKeyScrollIntent,
    onPointerDownIntent,
    onNestedScrollIntent,
    onScrollEnd: finishProgrammaticScroll,
    onSelectionPointerDown: selectionRetention.onPointerDownCapture,
  });
  const virtuosoResetKey = `${tabId ?? ""}:${revealSignal}`;
  const firstItemIndex = useTranscriptVirtuosoFirstItemIndex(virtualRows, virtuosoResetKey);
  const heightEstimates = useMemo(() => virtualRows.map((row) => estimateTranscriptRowSize(row)), [virtualRows]);
  const overlayRevision = useMemo(
    () => virtualRows.map((row) => String(row.key)).join("|"),
    [virtualRows],
  );
  const virtuosoContext = useMemo<TranscriptVirtuosoContext>(() => ({
    tabId,
    scrollElement,
    nativeScrollbarDragging,
    overlayRevision,
    olderHistory: hasOlderHistory
      ? {
          loading: loadingOlderHistory,
          label: loadingOlderHistory ? t("common.loading") : t("transcript.showEarlierHistory", { n: olderHistoryCount }),
          onLoad: onLoadOlderHistory,
        }
      : null,
  }), [hasOlderHistory, loadingOlderHistory, nativeScrollbarDragging, olderHistoryCount, onLoadOlderHistory, overlayRevision, scrollElement, t, tabId]);
  const handleScrollerRef = useCallback((node: HTMLElement | Window | null) => {
    scrollerRef(node);
    entranceRef.current = node instanceof HTMLElement ? node as HTMLDivElement : null;
  }, [entranceRef, scrollerRef]);
  const handleItemsRendered = useCallback((rendered: ListItem<TranscriptRow>[]) => {
    noteTranscriptRowCounts(rendered.length, virtualRows.length);
    selectionRetention.reconcileLogicalFocus();
    if (!virtuosoReadyRef.current && rendered.length > 0) {
      virtuosoReadyRef.current = true;
      requestAnimationFrame(() => scrollToBottom());
    }
  }, [scrollToBottom, selectionRetention.reconcileLogicalFocus, virtualRows.length]);

  // ── JumpBar integration ───────────────────────────────────────────────────
  const handleJumpToQuestion = useCallback((question: QuestionAnchor) => {
    const index = rowIndexByKey.get(String(userRowKey(question.id)));
    if (index == null) return;
    scrollToDataIndex(firstItemIndex, index, "smooth");
  }, [firstItemIndex, rowIndexByKey, scrollToDataIndex]);

  // After a non-fork rewind, scroll to the last user message (the
  // rewound-to point) so the user knows where they are.
  useEffect(() => {
    if (rewindSignal <= 0 || questions.length === 0) return;
    const lastQ = questions[questions.length - 1];
    const index = rowIndexByKey.get(String(userRowKey(lastQ.id)));
    if (index == null) return;
    scrollToDataIndex(firstItemIndex, index);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [rewindSignal]);

  const empty = items.length === 0;

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
            onAction={row.item.action === "continue_delivery"
              ? (onDeliveryContinue ?? (() => onPrompt(t("notice.deliveryIncompleteContinuePrompt"))))
              : row.item.action === "open_changes"
                ? onOpenChanges
                : undefined}
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
      {empty ? (
        <div
          className={`transcript transcript--empty${creationMode ? " transcript--creation-scrollbar" : ""}`}
          ref={(node) => handleScrollerRef(node)}
        >
          {!hydrating && <Welcome onPrompt={onPrompt} variant={welcomeVariant} />}
        </div>
      ) : (
        <LiveStreamContext.Provider value={live}>
          <Virtuoso<TranscriptRow, TranscriptVirtuosoContext>
            key={virtuosoResetKey}
            ref={virtuosoRef}
            className={`transcript${creationMode ? " transcript--creation-scrollbar" : ""}${creationMode && creationScrollbar.hot ? " transcript--scrollbar-hot" : ""}`}
            data-transcript-row-count={virtualRows.length}
            data={virtualRows}
            context={virtuosoContext}
            components={hasOlderHistory ? TRANSCRIPT_VIRTUOSO_COMPONENTS_WITH_HEADER : TRANSCRIPT_VIRTUOSO_COMPONENTS}
            computeItemKey={(_index, row) => `${tabId ?? ""}:${String(row.key)}`}
            firstItemIndex={firstItemIndex}
            // Do not set alignToBottom: Virtuoso's margin-top:auto plus
            // firstItemIndex paints a ghost first-user bubble and empty band
            // in short chats. Tail pin stays followOutput + scrollToBottom.
            followOutput={(atBottom) => atBottom ? "auto" : false}
            atBottomThreshold={TRANSCRIPT_AT_BOTTOM_THRESHOLD_PX}
            atBottomStateChange={atBottomStateChange}
            heightEstimates={heightEstimates}
            itemSize={itemSize}
            minOverscanItemCount={{ top: VIRTUAL_OVERSCAN_ROWS, bottom: VIRTUAL_OVERSCAN_ROWS }}
            increaseViewportBy={{ top: 480, bottom: 480 }}
            scrollerRef={handleScrollerRef}
            itemsRendered={handleItemsRendered}
            totalListHeightChanged={followGrowingTail}
            itemContent={(_index, row) => renderRow(row)}
            onScroll={creationMode ? handleCreationScroll : undefined}
            onWheelCapture={scrollInteractions.onWheelCapture}
            onTouchStartCapture={onTouchStartIntent}
            onTouchMoveCapture={scrollInteractions.onTouchMoveCapture}
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
        <QuestionJumpBar questions={questions} onJump={handleJumpToQuestion} />
      )}

      {!empty && !isAtBottom && (
        <button
          type="button"
          className="transcript__jump-bottom"
          onClick={() => scrollToBottom()}
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
  const ActionIcon = item.action === "open_changes" ? FileSearch : CirclePlay;
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
              <ActionIcon size={13} aria-hidden="true" />
              <span>{item.action === "open_changes" ? t("notice.completionViewChanges") : t("notice.deliveryIncompleteContinue")}</span>
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
