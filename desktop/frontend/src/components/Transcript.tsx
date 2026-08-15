import { forwardRef, memo, type CSSProperties, type PointerEvent as ReactPointerEvent, type ReactNode, useCallback, useContext, useEffect, useMemo, useRef, useState, useSyncExternalStore } from "react";
import { Virtuoso, type Components, type ItemProps, type ListItem, type ListProps } from "react-virtuoso";
import type { ControllerLiveStore, Item, LiveStream } from "../lib/useController";
import type { CheckpointMeta } from "../lib/types";
import type { InvocationMetadataMap } from "../lib/invocationDisplay";
import { useT } from "../lib/i18n";
import { AssistantMessage, InvocationMetadataContext, TurnActions, UserMessage } from "./Message";
import { ToolCard } from "./ToolCard";
import { ExtensionCard } from "./ExtensionCard";
import { ArrowDown } from "lucide-react";
import { Welcome } from "./Welcome";
import { ReadOnlyBatch } from "./ReadOnlyBatch";
import { ToolGroup } from "./ToolGroup";
import { getProcessFoldPreference, onProcessFoldPreferenceChange, type ProcessFoldPreference } from "../lib/processFoldPreference";
import { isSteerNoticeText } from "../lib/useController";
import { useTranscriptEntranceAnimation } from "../lib/useEntranceAnimation";
import { useTranscriptSelectionRetention } from "../lib/useTranscriptSelectionRetention";
import { compactQuestionText, lastQuestionTurn, questionAnchorId, questionTurnsById, type QuestionAnchor } from "../lib/transcriptGrouping";
import {
  buildTranscriptRows,
  buildTurnModels,
  foldMapWithReasoningOpen,
  foldMapWithToggle,
  foldSegmentStates,
  historyEntryIdForRow,
  reconcileFoldEntries,
  estimateTranscriptRowSize,
  splitTranscriptLiveRows,
  userRowKey,
  EMPTY_FOLDS,
  NO_LIVE,
  type AssistantItem,
  type FoldMap,
  type ToolItem,
  type TranscriptLiveFlags,
  type TranscriptRow,
} from "../lib/transcriptRows";
import { getTranscriptStore } from "../lib/transcriptStore";
import { acquireMarkdownWorkerClient, releaseMarkdownWorkerClient } from "../lib/markdownWorkerClient";
import { noteTranscriptRowCounts } from "../lib/sessionDiagnostics";
import { useReasoningDisplayMode } from "../lib/reasoningDisplayPreference";
import { InlineAssistantReasoning } from "./InlineAssistantReasoning";
import { LiveTurnRegion } from "./LiveTurnRegion";
import { ProcessFoldHeader } from "./ProcessFoldHeader";
import { QuestionJumpBar } from "./QuestionJumpBar";
import { CompactionCard, NoticeCard, PhaseCard, SteerCard } from "./TranscriptCards";
import { LiveStreamContext } from "./LiveStreamContext";
import { useTranscriptSelectableRows } from "../lib/useTranscriptSelectableRows";
import { TranscriptSelectionOverlay } from "./TranscriptSelectionOverlay";
import { useCreationTranscriptScrollbar } from "../lib/useCreationTranscriptScrollbar";
import { useTranscriptScrollInteractions } from "../lib/useTranscriptScrollInteractions";
import { hasTranscriptScrollableRange, TRANSCRIPT_AT_BOTTOM_THRESHOLD_PX, useTranscriptVirtuosoScroll } from "../lib/useTranscriptVirtuosoScroll";
import { useTranscriptVirtuosoRecovery } from "../lib/useTranscriptVirtuosoRecovery";
import { TranscriptLayoutIntentProvider } from "./TranscriptLayoutIntentContext";
import { MarkdownImageTabContext } from "./MarkdownImageContext";

// NoticeCard lives with the other row cards; keep the historical export path.
export { NoticeCard } from "./TranscriptCards";
type OpenTurnAction = { turn: number; menu: "summary" | "rewind" };
const QUESTION_NAV_MIN_COUNT = 2;
type AssistantReasoningDisplay = "normal" | "hide";
const EMPTY_CHECKPOINTS: CheckpointMeta[] = [];
const EMPTY_INVOCATION_METADATA: InvocationMetadataMap = {};
const NO_HELD_ROWS: readonly TranscriptRow[] = [];

const LiveAssistantMessage = memo(function LiveAssistantMessage({
  item,
  defaultExpanded = false,
  expandWhileStreaming = false,
  creationMode = false,
  reasoningDisplay = "normal",
}: {
  item: AssistantItem;
  defaultExpanded?: boolean;
  expandWhileStreaming?: boolean;
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
  /** The active turn's in-flow footer region; null when no turn is live. */
  liveRegion: null | {
    rows: readonly TranscriptRow[];
    renderRow: (row: TranscriptRow) => ReactNode;
    showStatus: boolean;
    turnStartAt?: number;
    onPointerDownCapture: (event: ReactPointerEvent<HTMLElement>) => void;
  };
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

// The live turn region is the list's in-flow Footer: it scrolls with the
// transcript but is never part of Virtuoso's measured size tree.
function TranscriptVirtuosoFooter({ context }: { context: TranscriptVirtuosoContext }) {
  const live = context.liveRegion;
  if (!live || (live.rows.length === 0 && !live.showStatus)) return null;
  return (
    <LiveTurnRegion
      rows={live.rows}
      renderRow={live.renderRow}
      showStatus={live.showStatus}
      turnStartAt={live.turnStartAt}
      tabId={context.tabId}
      scrollElement={context.scrollElement}
      onPointerDownCapture={live.onPointerDownCapture}
    />
  );
}

const TRANSCRIPT_VIRTUOSO_COMPONENTS: Components<TranscriptRow, TranscriptVirtuosoContext> = {
  Item: TranscriptVirtuosoItem,
  List: TranscriptVirtuosoList,
  Footer: TranscriptVirtuosoFooter,
};

const TRANSCRIPT_VIRTUOSO_COMPONENTS_WITH_HEADER: Components<TranscriptRow, TranscriptVirtuosoContext> = {
  ...TRANSCRIPT_VIRTUOSO_COMPONENTS,
  Header: TranscriptVirtuosoHeader,
};

// ── Helpers ───────────────────────────────────────────────────────────────────

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
  historyLayoutRevision = 0,
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
  historyLayoutRevision?: number;
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
  const liveTailActiveRef = useRef(false);
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
    beginUserResize,
    scrollToDataIndex,
    releaseTailFollow,
    setMode: setScrollMode,
    writeOffset,
    reset: resetScroll,
    finishProgrammaticScroll,
  } = useTranscriptVirtuosoScroll({ liveTailActiveRef });
  const virtuosoReadyRef = useRef(false);
  const layoutSurfaceKey = `${tabId ?? ""}:${revealSignal}`;

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

  // Row measurement and footer resize share the same coalesced height path.
  useEffect(() => {
    if (!virtuosoReadyRef.current || !stick.current) return;
    followGrowingTail();
  }, [footerHeight, followGrowingTail, stick]);

  // The live region grows from zero height and shrinks the history viewport
  // mid-stream; keep the tail pinned across that viewport resize.
  useEffect(() => {
    const element = scrollElement;
    if (!element || typeof ResizeObserver === "undefined") return;
    let lastHeight = element.clientHeight;
    const observer = new ResizeObserver(() => {
      const height = element.clientHeight;
      if (height === lastHeight) return;
      lastHeight = height;
      followGrowingTail();
    });
    observer.observe(element);
    return () => observer.disconnect();
  }, [scrollElement, followGrowingTail]);

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

  const userTurn = useMemo(() => questionTurnsById(questions), [questions]);
  const lastTurn = useMemo(() => lastQuestionTurn(questions, userTurn), [questions, userTurn]);
  const checkpointsByTurn = useMemo(() => new Map(checkpoints.map((checkpoint) => [checkpoint.turn, checkpoint])), [checkpoints]);
  const hasCheckpointForTurn = useCallback((turn: number) => checkpointsByTurn.has(turn), [checkpointsByTurn]);

  const turnForUser = useCallback((item: Extract<Item, { kind: "user" }>) => userTurn.get(item.id), [userTurn]);
  const rows = useMemo(
    () => buildTranscriptRows(turnModels, { folds, foldPreference, hasOlderHistory, creationMode, turnForUser, hasCheckpointForTurn }),
    [turnModels, folds, foldPreference, hasOlderHistory, creationMode, turnForUser, hasCheckpointForTurn],
  );
  // The active (streaming) turn renders as the list's in-flow Footer, outside
  // the measured size tree: the list only ever owns static, bounded rows, so
  // streaming never churns Virtuoso's measurements or scroll anchoring
  // (#8657/#8688).
  const liveSplit = useMemo(
    () => splitTranscriptLiveRows(turnModels, rows, liveId, running),
    [turnModels, rows, liveId, running],
  );
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
  const {
    resetKey: virtuosoResetKey,
    firstItemIndex,
    restoreLocation,
    handleItemsRendered: handleRecoveryItemsRendered,
    scheduleBlankViewportCheck,
    invalidateAnchors,
  } = useTranscriptVirtuosoRecovery({
    surfaceKey: layoutSurfaceKey,
    historyLayoutRevision,
    rows: virtualRows,
    rowIndexByKey,
    scrollRef,
    pinnedRef: stick,
    virtuosoRef,
    readyRef: virtuosoReadyRef,
    scrollToBottom,
  });
  const heightEstimates = useMemo(() => virtualRows.map((row) => estimateTranscriptRowSize(row)), [virtualRows]);
  const overlayRevision = useMemo(
    () => virtualRows.map((row) => String(row.key)).join("|"),
    [virtualRows],
  );
  const handleScrollerRef = useCallback((node: HTMLElement | Window | null) => {
    scrollerRef(node);
    entranceRef.current = node instanceof HTMLElement ? node as HTMLDivElement : null;
  }, [entranceRef, scrollerRef]);
  const handleTranscriptScroll = useCallback(() => {
    if (creationMode) handleCreationScroll();
    scheduleBlankViewportCheck();
  }, [creationMode, handleCreationScroll, scheduleBlankViewportCheck]);
  // ── JumpBar integration ───────────────────────────────────────────────────
  const handleJumpToQuestion = useCallback((question: QuestionAnchor) => {
    const index = rowIndexByKey.get(String(userRowKey(question.id)));
    if (index == null) return;
    invalidateAnchors();
    scrollToDataIndex(firstItemIndex, index, "smooth");
  }, [firstItemIndex, invalidateAnchors, rowIndexByKey, scrollToDataIndex]);

  // The jump-bottom click is explicit user intent: it outranks any in-flight
  // recovery anchor restore and ends a stale selection gesture whose
  // pointerup was lost (#8657/#8688).
  const handleJumpToBottom = () => {
    selectionRetention.endStaleGesture();
    invalidateAnchors();
    scrollToBottom();
  };

  // After a non-fork rewind, scroll to the last user message (the
  // rewound-to point) so the user knows where they are.
  useEffect(() => {
    if (rewindSignal <= 0 || questions.length === 0) return;
    const lastQ = questions[questions.length - 1];
    const index = rowIndexByKey.get(String(userRowKey(lastQ.id)));
    if (index == null) return;
    invalidateAnchors();
    scrollToDataIndex(firstItemIndex, index);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [rewindSignal]);

  const empty = items.length === 0;

  // ── Row rendering ─────────────────────────────────────────────────────────
  // renderRow/itemContent keep stable identities: Transcript re-renders on
  // every streaming frame, and Virtuoso re-maps every mounted row whenever
  // itemContent changes identity.
  const renderRow = useCallback((row: TranscriptRow): ReactNode => {
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
  }, [
    actionHoverMenus,
    actionPending,
    checkpointsByTurn,
    creationMode,
    handleFoldToggle,
    handleReasoningManualOpen,
    lastTurn,
    loadingOlderHistory,
    olderHistoryCount,
    onDeliveryContinue,
    onEditPrompt,
    onLoadOlderHistory,
    onOpenChanges,
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
  const heldLiveRows = heldSurfaceRef.current === layoutSurfaceKey ? heldLiveRowsRef.current : NO_HELD_ROWS;
  const showLiveRegion = liveSplit.liveActive || (holdingLiveRegion && heldLiveRows.length > 0);
  liveTailActiveRef.current = showLiveRegion;

  const handleItemsRendered = useCallback((rendered: ListItem<TranscriptRow>[]) => {
    noteTranscriptRowCounts(rendered.length, virtualRows.length);
    selectionRetention.reconcileLogicalFocus();
    handleRecoveryItemsRendered(rendered.length);
    if (holdingLiveRegion) {
      const held = heldLiveRowsRef.current;
      const lastKey = held.length > 0 ? String(held[held.length - 1].key) : null;
      if (lastKey === null || rendered.some((item) => String(item.data?.key ?? "") === lastKey)) {
        heldLiveRowsRef.current = [];
        setHoldingLiveRegion(false);
      }
    }
  }, [handleRecoveryItemsRendered, holdingLiveRegion, selectionRetention.reconcileLogicalFocus, virtualRows.length]);

  const virtuosoContext = useMemo<TranscriptVirtuosoContext>(() => ({
    tabId,
    scrollElement,
    nativeScrollbarDragging,
    overlayRevision,
    liveRegion: showLiveRegion
      ? {
          rows: liveSplit.liveActive ? liveSplit.liveRows : heldLiveRows,
          renderRow,
          showStatus: liveSplit.liveActive,
          turnStartAt,
          onPointerDownCapture: selectionRetention.onPointerDownCapture,
        }
      : null,
    olderHistory: hasOlderHistory
      ? {
          loading: loadingOlderHistory,
          label: loadingOlderHistory ? t("common.loading") : t("transcript.showEarlierHistory", { n: olderHistoryCount }),
          onLoad: onLoadOlderHistory,
        }
      : null,
  }), [
    hasOlderHistory,
    heldLiveRows,
    liveSplit.liveActive,
    liveSplit.liveRows,
    loadingOlderHistory,
    nativeScrollbarDragging,
    olderHistoryCount,
    onLoadOlderHistory,
    overlayRevision,
    renderRow,
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
            initialTopMostItemIndex={restoreLocation}
            // Do not set alignToBottom: Virtuoso's margin-top:auto plus
            // firstItemIndex paints a ghost first-user bubble and empty band
            // in short chats. The coordinator owns tail following.
            atBottomThreshold={TRANSCRIPT_AT_BOTTOM_THRESHOLD_PX}
            atBottomStateChange={atBottomStateChange}
            heightEstimates={heightEstimates}
            itemSize={itemSize}
            minOverscanItemCount={{ top: VIRTUAL_OVERSCAN_ROWS, bottom: VIRTUAL_OVERSCAN_ROWS }}
            increaseViewportBy={{ top: 480, bottom: 480 }}
            scrollerRef={handleScrollerRef}
            itemsRendered={handleItemsRendered}
            totalListHeightChanged={followGrowingTail}
            itemContent={renderVirtuosoRow}
            onScroll={handleTranscriptScroll}
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
    </div>
    </TranscriptLayoutIntentProvider>
    </MarkdownImageTabContext.Provider>
    </InvocationMetadataContext.Provider>
  );
}
