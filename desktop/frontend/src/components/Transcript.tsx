import { createContext, memo, type CSSProperties, type MouseEvent as ReactMouseEvent, type ReactNode, useCallback, useContext, useEffect, useMemo, useRef, useState } from "react";
import type { Item, LiveStream } from "../lib/useController";
import type { CheckpointMeta } from "../lib/types";
import { useT } from "../lib/i18n";
import { replaceAttachmentRefsForDisplay } from "../lib/attachmentDisplay";
import { AssistantMessage, TurnActions, UserMessage } from "./Message";
import { ProcessCompactIcon, ProcessPhaseIcon } from "./ProcessCard";
import { ToolCard } from "./ToolCard";
import { ChevronRight } from "lucide-react";
import { Welcome } from "./Welcome";
import { ReadOnlyBatch } from "./ReadOnlyBatch";
import { getDisplayMode, onDisplayModeChange, type DisplayMode } from "../lib/displayMode";
import { isReadOnlyTool } from "../lib/useController";
import { useGSAPCollapse } from "../lib/useGSAPCollapse";
import { useEntranceAnimation } from "../lib/useEntranceAnimation";
import { useScrollManager } from "../lib/useScrollManager";

type ToolItem = Extract<Item, { kind: "tool" }>;
type AssistantItem = Extract<Item, { kind: "assistant" }>;
type OpenTurnAction = { turn: number; menu: "summary" | "rewind" };
type QuestionAnchor = { id: string; text: string; turn: number };

const QUESTION_NAV_MIN_COUNT = 2;
const LiveStreamContext = createContext<LiveStream | undefined>(undefined);

const LiveAssistantMessage = memo(function LiveAssistantMessage({ item, defaultExpanded = false }: { item: AssistantItem; defaultExpanded?: boolean }) {
  const live = useContext(LiveStreamContext);
  const shown = live && live.id === item.id ? { ...item, text: live.text, reasoning: live.reasoning, streaming: true } : item;
  return <AssistantMessage item={shown} defaultExpanded={defaultExpanded} />;
});

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

function questionAnchorId(id: string): string {
  return `question-anchor-${id}`;
}

function compactQuestionText(text: string): string {
  const cleaned = replaceAttachmentRefsForDisplay(text).replace(/\s+/g, " ").trim();
  if (cleaned.length <= 80) return cleaned;
  return cleaned.slice(0, 80);
}

function scrollVersion(items: Item[]): string {
  return items
    .map((it) => {
      switch (it.kind) {
        case "assistant":
          return `${it.id}:a:${it.text?.length ?? 0}:${it.reasoning?.length ?? 0}:${it.streaming ? 1 : 0}`;
        case "tool":
          return `${it.id}:t:${it.name}:${it.status}:${it.args?.length ?? 0}:${it.output?.length ?? 0}:${it.error?.length ?? 0}:${it.truncated ? 1 : 0}`;
        default:
          return `${it.id}:${it.kind}`;
      }
    })
    .join("|");
}

// Summarise a warm turn for its compact card.
function warmUserPreview(text: string): string {
  const cleaned = replaceAttachmentRefsForDisplay(text).replace(/\s+/g, " ").trim();
  return cleaned.length <= 80 ? cleaned : cleaned.slice(0, 77) + "...";
}

// ── Turn grouping ─────────────────────────────────────────────────────────────
// A turn is everything from one UserMessage up to (but not including) the next
// UserMessage. This grouping is used only for warm-zone rendering; the hot zone
// still uses the flat items array to preserve the existing rendering logic.

interface TurnGroup {
  userItem: Item;
  assistantPreview: string;
  toolCount: number;
  startIdx: number; // first index in items[] (the user message)
  endIdx: number;   // exclusive end
}

function buildTurnGroups(items: Item[], questions: QuestionAnchor[]): TurnGroup[] {
  const groups: TurnGroup[] = [];
  let turnIdx = 0;
  let start = -1;
  for (let i = 0; i < items.length; i++) {
    if (items[i].kind === "user") {
      if (start >= 0) {
        // finalise previous turn
        groups[groups.length - 1].endIdx = i;
      }
      start = i;
      turnIdx = questions.findIndex((q) => q.id === items[i].id);
      if (turnIdx < 0) turnIdx = groups.length;
      groups.push({
        userItem: items[i],
        assistantPreview: "",
        toolCount: 0,
        startIdx: i,
        endIdx: items.length,
      });
    } else if (start >= 0 && groups.length > 0) {
      const g = groups[groups.length - 1];
      const it = items[i];
      if (it.kind === "assistant" && !it.streaming) {
        const previewText = it.text?.trim() || "";
        if (previewText) {
          g.assistantPreview = warmUserPreview(previewText);
        }
      }
      if (it.kind === "tool" && !it.parentId) {
        g.toolCount++;
      }
    }
  }
  return groups;
}

// ── Transcript component ──────────────────────────────────────────────────────

export function Transcript({
  items,
  live,
  footerHeight = 0,
  onPrompt,
  onRewind,
  checkpoints = [],
  actionPending = false,
  rewindDisabled = false,
  questionNavigator = true,
  rewindSignal = 0,
}: {
  items: Item[];
  live?: LiveStream;
  footerHeight?: number;
  onPrompt: (text: string) => void;
  onRewind?: (turn: number, scope: string) => void;
  checkpoints?: CheckpointMeta[];
  actionPending?: boolean;
  rewindDisabled?: boolean;
  questionNavigator?: boolean;
  rewindSignal?: number;
}) {
  const {
    scrollRef,
    stick,
    onScroll,
    smoothScrollTo,
    trackQuestions,
    repinIfWasPinned,
    resizeFrame,
    lastClientHeight,
    lastFooterHeight,
  } = useScrollManager();
  const sessionKey = useMemo(() => `${items[0]?.id ?? ""}|${items[items.length - 1]?.id ?? ""}`, [items]);
  const entranceRef = useEntranceAnimation<HTMLDivElement>(sessionKey, items.length);

  const [displayMode, setDisplayMode] = useState<DisplayMode>(() => getDisplayMode());
  useEffect(() => onDisplayModeChange((mode) => setDisplayMode(mode)), []);

  const questions = useMemo<QuestionAnchor[]>(() => {
    const anchors: QuestionAnchor[] = [];
    let turn = 0;
    for (const it of items) {
      if (it.kind !== "user") continue;
      anchors.push({ id: it.id, text: compactQuestionText(it.text), turn });
      turn += 1;
    }
    return anchors;
  }, [items]);
  const showQuestionNav = questionNavigator && questions.length >= QUESTION_NAV_MIN_COUNT;

  // Track question count and auto-scroll on new messages.
  useEffect(() => { trackQuestions(questions.length); }, [questions.length, trackQuestions]);

  // Auto-scroll to bottom during streaming — instant, no GSAP tween.
  // A 120ms tween during fast streaming is perpetually killed/restarted
  // before reaching the target, causing visible jitter.  Direct scrollTop
  // assignment is synchronous and always hits the exact bottom.
  const contentVersion = useMemo(() => scrollVersion(items), [items]);
  useEffect(() => {
    if (!stick.current) return;
    const el = scrollRef.current;
    if (el) el.scrollTop = el.scrollHeight;
  }, [contentVersion, live?.text?.length ?? 0, live?.reasoning?.length ?? 0]);

  // ResizeObserver for container height changes.
  useEffect(() => {
    const el = scrollRef.current;
    if (!el || typeof ResizeObserver === "undefined") return;
    lastClientHeight.current = el.clientHeight;
    const observer = new ResizeObserver(() => {
      const previous = lastClientHeight.current ?? el.clientHeight;
      lastClientHeight.current = el.clientHeight;
      repinIfWasPinned(el.clientHeight - previous);
    });
    observer.observe(el);
    return () => {
      observer.disconnect();
      if (resizeFrame.current !== null) {
        cancelAnimationFrame(resizeFrame.current);
        resizeFrame.current = null;
      }
    };
  }, []);

  // Footer height changes → smooth scroll repin with GSAP.
  useEffect(() => {
    const el = scrollRef.current;
    if (!el) return;
    const previous = lastFooterHeight.current ?? footerHeight;
    lastFooterHeight.current = footerHeight;
    repinIfWasPinned(previous - footerHeight);
  }, [footerHeight]);

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
  const [expandedWarmTurns, setExpandedWarmTurns] = useState<Set<number>>(new Set());
  const [coldPage, setColdPage] = useState(0);

  // Compute turn groups (memoised — only rebuilds when user turns change,
  // not on every streaming token). The warm previews are static once built.
  const turnGroupKey = questions.length;
  const turnGroups = useMemo(() => buildTurnGroups(items, questions), [turnGroupKey, questions]);

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
  const warmTurnCount = turnGroups.length - Math.min(turnGroups.length, HOT_TURNS);
  const shownWarmStart = Math.max(0, warmTurnCount - coldPage * WARM_PAGE_SIZE);
  const coldTurnCount = shownWarmStart;

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

  const userTurn = useMemo(() => new Map(questions.map((question) => [question.id, question.turn])), [questions]);
  const checkpointsByTurn = useMemo(() => new Map(checkpoints.map((checkpoint) => [checkpoint.turn, checkpoint])), [checkpoints]);

  // ── JumpBar integration ───────────────────────────────────────────────────
  const jumpToQuestion = (question: QuestionAnchor) => {
    const node = document.getElementById(questionAnchorId(question.id));
    if (!node) return;
    stick.current = false;
    smoothScrollTo(node, 12);
  };

  const handleJumpToQuestion = useCallback((question: QuestionAnchor) => {
    // Auto-expand the warm turn when jumping to an old question.
    const warmTurnStart = turnGroups.length - HOT_TURNS;
    if (question.turn < warmTurnStart) {
      setExpandedWarmTurns((prev) => {
        if (prev.has(question.turn)) return prev;
        return new Set([...prev, question.turn]);
      });
    }
    jumpToQuestion(question);
  }, [turnGroups.length]);

  // ── Hot zone: fully rendered from hotStartIdx to end ─────────────────────
  // Memoized separately from the assembly so streaming tokens don't rebuild
  // the warm/cold zone JSX trees. Uses LiveStreamContext for streaming data
  // (added by upstream PR #3423) instead of per-call renderSegments.
  const empty = items.length === 0;

  // In compact mode, break each turn into step groups.
  // A step = one assistant + its tool results, from one assistant to the next.
  // Each completed non-final step is folded into "Processed".
  const stepGroups = useMemo(() => {
    if (displayMode === "standard") return null;
    const groups: { items: Item[]; isFinal: boolean; isComplete: boolean }[] = [];
    let current: Item[] = [];

    for (let i = hotStartIdx; i < items.length; i++) {
      const it = items[i];
      if (it.kind === "user") {
        if (current.length > 0) {
          const first = current[0];
          const isFinal = first.kind === "assistant" && !first.streaming && first.text.trim() !== "";
          groups.push({ items: current, isFinal, isComplete: true });
          current = [];
        }
        groups.push({ items: [it], isFinal: false, isComplete: true });
        continue;
      }
      if (it.kind === "assistant") {
        if (current.length > 0) {
          groups.push({ items: current, isFinal: false, isComplete: true });
          current = [];
        }
        current.push(it);
      } else {
        current.push(it);
      }
    }
    if (current.length > 0) {
      const first = current[0];
      const isFinal = first.kind === "assistant" && !first.streaming && first.text.trim() !== "";
      groups.push({ items: current, isFinal, isComplete: false });
    }
    return groups;
  }, [displayMode, hotStartIdx, items]);

  const hotZoneNodes = useMemo<ReactNode[]>(() => {
    const out: ReactNode[] = [];
    let actionText = "";
    let actionReady = false;
    let activeTurn: number | undefined;
    const pushTurnActions = () => {
      if (activeTurn == null || !actionReady || actionText.trim() === "") return;
      const turn = activeTurn;
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
          onRewind={(targetTurn, scope) => {
            onRewind?.(targetTurn, scope);
            setOpenAction(null);
          }}
        />,
      );
      actionText = "";
      actionReady = false;
    };

    // Compact mode: step-based rendering
    // Standard mode: flat rendering (no step groups)
    if (stepGroups) {
      // Collect consecutive completed non-final steps into batches
      let collapseBatch: Item[] = [];
      let collapseBatchStart: string | null = null;
      const flushCollapseBatch = () => {
        if (collapseBatch.length === 0) return;
        const dur = collapseBatch.reduce((ms, it) => ms + (it.kind === "tool" ? it.durationMs ?? 0 : 0), 0);
        out.push(
          <TurnCollapse
            key={`step-batch-${collapseBatchStart}`}
            items={collapseBatch}
            durationMs={dur}
            mode={displayMode}
            subcalls={subcallsByParent}
          />,
        );
        collapseBatch = [];
        collapseBatchStart = null;
      };

      for (const group of stepGroups) {
        const first = group.items[0];

        if (first.kind === "user") {
          flushCollapseBatch();
          pushTurnActions();
          const tn = userTurn.get(first.id);
          activeTurn = tn;
          out.push(
            <UserMessage key={first.id} id={first.id} text={first.text} failed={first.failed} turn={tn} anchorId={questionAnchorId(first.id)} />,
          );
          continue;
        }

        // Completed non-final step → batch it
        if (group.isComplete && !group.isFinal) {
          if (!collapseBatchStart) collapseBatchStart = first.id;
          collapseBatch.push(...group.items);
          continue;
        }

        // Final answer or active step → flush any pending batch then render
        flushCollapseBatch();
        const nonAssistantItems = group.items.filter(
          (it) => it.kind !== "assistant" || (it.streaming && !it.text.trim())
        );
        const hasRunning = nonAssistantItems.some((it) => it.kind === "tool" && it.status === "running");
        if (nonAssistantItems.length > 0 && !hasRunning) {
          const dur = nonAssistantItems.reduce((ms, it) => ms + (it.kind === "tool" ? ((it as ToolItem).durationMs ?? 0) : 0), 0);
          out.push(
            <TurnCollapse
              key={`step-${first.id}`}
              items={nonAssistantItems}
              durationMs={dur}
              mode={displayMode}
              subcalls={subcallsByParent}
            />,
          );
        } else if (nonAssistantItems.length > 0) {
          for (const it of nonAssistantItems) {
            if (it.kind === "tool") {
              if (it.parentId) continue;
              if (it.name === "todo_write" || it.name === "exit_plan_mode") continue;
              out.push(<ToolCard key={it.id} item={it as ToolItem} subcalls={subcallsByParent.get(it.id)} />);
            }
            if (it.kind === "phase") out.push(<PhaseCard key={it.id} text={it.text} />);
          }
        }
        // Render the final assistant message (if any) directly
        for (const it of group.items) {
          if (it.kind !== "assistant") continue;
          out.push(<LiveAssistantMessage key={it.id} item={it as AssistantItem} defaultExpanded={false} />);
          if (!it.streaming && it.text.trim() !== "") {
            actionText = it.text;
            actionReady = true;
          }
        }
      }
      flushCollapseBatch();
      pushTurnActions();
    } else {
      // Standard mode: flat rendering
      const roBatch: ToolItem[] = [];
      const flushRO = () => {
        if (roBatch.length === 0) return;
        out.push(<ReadOnlyBatch key={`rob-${roBatch[0].id}`} items={[...roBatch]} subcalls={subcallsByParent} />);
        roBatch.length = 0;
      };
      for (let i = hotStartIdx; i < items.length; i++) {
        const it = items[i];
        if (
          it.kind === "tool" &&
          !it.parentId &&
          it.status !== "running" &&
          it.name !== "todo_write" &&
          it.name !== "exit_plan_mode" &&
          isReadOnlyTool(it.name)
        ) {
          roBatch.push(it as ToolItem);
          continue;
        }
        flushRO();
        switch (it.kind) {
          case "user": {
            pushTurnActions();
            const tn = userTurn.get(it.id);
            activeTurn = tn;
            out.push(
              <UserMessage key={it.id} id={it.id} text={it.text} failed={it.failed} turn={tn} anchorId={questionAnchorId(it.id)} />,
            );
            break;
          }
          case "assistant":
            out.push(<LiveAssistantMessage key={it.id} item={it as AssistantItem} defaultExpanded={false} />);
            if (!it.streaming && it.text.trim() !== "") {
              actionText = it.text;
              actionReady = true;
            }
            break;
          case "tool":
            if (it.parentId) break;
            if (it.name === "todo_write") break;
            if (it.name === "exit_plan_mode") break;
            out.push(<ToolCard key={it.id} item={it} subcalls={subcallsByParent.get(it.id)} />);
            break;
          case "phase": out.push(<PhaseCard key={it.id} text={it.text} />); break;
          case "notice": out.push(<NoticeCard key={it.id} level={it.level} text={it.text} />); break;
          case "compaction": out.push(<CompactionCard key={it.id} item={it} />); break;
        }
      }
      flushRO();
      pushTurnActions();
    }
    return out;
  }, [hotStartIdx, items, openAction, actionPending, rewindDisabled, onRewind, subcallsByParent, userTurn, checkpointsByTurn, displayMode, stepGroups]);

  // ── Assemble rendered output ──────────────────────────────────────────────
  // Warm/cold zone is a separate memo'd WarmZone component so streaming tokens
  // don't rebuild it. The hot zone uses LiveAssistantMessage (reads live from
  // LiveStreamContext) so streaming updates are captured immediately.
  return (
    <div
      className={`transcript${empty ? " transcript--empty" : ""}`}
      ref={scrollRef}
      onScroll={onScroll}
    >
      {empty && <Welcome onPrompt={onPrompt} />}

      {!empty && showQuestionNav && (
        <QuestionJumpBar questions={questions} onJump={handleJumpToQuestion} />
      )}

      <LiveStreamContext.Provider value={live}>
        {turnGroups.length > HOT_TURNS && (
          <WarmZone
            turnGroups={turnGroups}
            expandedWarmTurns={expandedWarmTurns}
            shownWarmStart={shownWarmStart}
            coldTurnCount={coldTurnCount}
            scrollRef={scrollRef}
            warmItems={items}
            warmSubcalls={subcallsByParent}
            warmUserTurn={userTurn}
            warmCheckpoints={checkpointsByTurn}
            warmOpenAction={openAction}
            warmActionPending={actionPending}
            warmRewindDisabled={rewindDisabled}
            warmOnRewind={onRewind}
            warmSetOpenAction={setOpenAction}
            onToggleColdPage={() => setColdPage((p) => p + 1)}
            onToggleWarmTurn={(g, expand) => {
              setExpandedWarmTurns((prev) => {
                const next = new Set(prev);
                if (expand) next.add(g); else next.delete(g);
                return next;
              });
            }}
          />
        )}
        <div ref={entranceRef}>
          {hotZoneNodes}
        </div>
      </LiveStreamContext.Provider>
    </div>
  );
}

// ── WarmZone sub-component (React.memo for streaming isolation) ────────────
// Receives structural props only; reads streaming state (items, live) via refs
// so it never invalidates on streaming token arrival.

const WarmZone = memo(function WarmZone({
  turnGroups,
  expandedWarmTurns,
  shownWarmStart,
  coldTurnCount,
  scrollRef,
  warmItems,
  warmSubcalls,
  warmUserTurn,
  warmCheckpoints,
  warmOpenAction,
  warmActionPending,
  warmRewindDisabled,
  warmOnRewind,
  warmSetOpenAction,
  onToggleColdPage,
  onToggleWarmTurn,
}: {
  turnGroups: TurnGroup[];
  expandedWarmTurns: ReadonlySet<number>;
  shownWarmStart: number;
  coldTurnCount: number;
  scrollRef: React.RefObject<HTMLDivElement | null>;
  warmItems: readonly Item[];
  warmSubcalls: ReadonlyMap<string, ToolItem[]>;
  warmUserTurn: ReadonlyMap<string, number>;
  warmCheckpoints: ReadonlyMap<number, CheckpointMeta>;
  warmOpenAction: OpenTurnAction | null;
  warmActionPending: boolean;
  warmRewindDisabled: boolean;
  warmOnRewind: ((turn: number, scope: string) => void) | undefined;
  warmSetOpenAction: (action: OpenTurnAction | null) => void;
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
  let warmStartTurn = 0;
  if (turnGroups.length > HOT_TURNS) {
    warmStartTurn = turnGroups.length - HOT_TURNS - shownWarmStart;
    for (let g = warmStartTurn; g < turnGroups.length - HOT_TURNS; g++) {
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
              onRewind={warmOnRewind}
              setOpenAction={warmSetOpenAction}
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
  onRewind,
  setOpenAction,
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
  onRewind: ((turn: number, scope: string) => void) | undefined;
  setOpenAction: (action: OpenTurnAction | null) => void;
}) {
  const nodes: React.ReactNode[] = [];
  let actionText = "";
  let actionReady = false;
  let activeTurn: number | undefined;
  const pushTurnActions = () => {
    if (activeTurn == null || !actionReady || actionText.trim() === "") return;
    const turn = activeTurn;
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
        onRewind={(targetTurn, scope) => {
          onRewind?.(targetTurn, scope);
          setOpenAction(null);
        }}
      />,
    );
    actionText = "";
    actionReady = false;
  };

  // Group consecutive completed read-only tools into ReadOnlyBatch
  const roBatch: ToolItem[] = [];
  const flushRO = () => {
    if (roBatch.length === 0) return;
    nodes.push(<ReadOnlyBatch key={`rob-${roBatch[0].id}`} items={[...roBatch]} subcalls={subcalls} />);
    roBatch.length = 0;
  };

  for (let i = startIdx; i < endIdx && i < items.length; i++) {
    const it = items[i];

    // Completed read-only tools → batch into ReadOnlyBatch
    if (it.kind === "tool" && !it.parentId && it.name !== "todo_write" && it.name !== "exit_plan_mode" && isReadOnlyTool(it.name)) {
      roBatch.push(it as ToolItem);
      continue;
    }
    flushRO();

    switch (it.kind) {
      case "user": {
        pushTurnActions();
        const tn = userTurnMap.get(it.id);
        activeTurn = tn;
        nodes.push(
          <UserMessage key={it.id} text={it.text} failed={it.failed} turn={tn} anchorId={questionAnchorId(it.id)} />,
        );
        break;
      }
      case "assistant": {
        nodes.push(<AssistantMessage key={it.id} item={it} defaultExpanded={false} />);
        if (!it.streaming && it.text.trim() !== "") {
          actionText = it.text;
          actionReady = true;
        }
        break;
      }
      case "tool": {
        if (it.parentId) break;
        if (it.name === "todo_write") break;
        if (it.name === "exit_plan_mode") break;
        nodes.push(<ToolCard key={it.id} item={it} subcalls={subcalls.get(it.id)} />);
        break;
      }
      case "phase": nodes.push(<PhaseCard key={it.id} text={it.text} />); break;
      case "notice": nodes.push(<NoticeCard key={it.id} level={it.level} text={it.text} />); break;
      case "compaction": nodes.push(<CompactionCard key={it.id} item={it} />); break;
    }
  }
  flushRO();
  pushTurnActions();
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

// ── TurnCollapse: compact mode grouping ──────────────────────────────────────

type TurnCollapseProps = {
  items: Item[];       // intermediate items (tools, reasoning, phase)
  durationMs: number;  // summed tool execution time across the batch; 0 when unknown
  mode: DisplayMode;
  subcalls: Map<string, ToolItem[]>;
};

function TurnCollapse({ items, durationMs, mode, subcalls }: TurnCollapseProps) {
  const t = useT();
  const [open, setOpen] = useState(false);
  const bodyRef = useRef<HTMLDivElement>(null);
  useGSAPCollapse(bodyRef, open);

  // Keep only items the body will actually render — an expandable fold over
  // nothing is worse than no fold.
  const displayItems = useMemo(() => {
    return items.filter((it) => {
      if (it.kind === "assistant") {
        if (it.text.trim() !== "") return true;
        return Boolean(it.reasoning);
      }
      if (it.kind === "phase") return true;
      if (it.kind !== "tool") return false;
      if (it.parentId || it.name === "todo_write" || it.name === "exit_plan_mode") return false;
      return true;
    });
  }, [items, mode]);

  const seconds = Math.round(durationMs / 1000);
  const label = seconds > 0 ? t("transcript.processedDuration", { s: seconds }) : t("transcript.processed");

  if (displayItems.length === 0) return null;

  // Pre-compute body: group consecutive completed read-only tools into ReadOnlyBatch
  const body: ReactNode[] = [];
  const roBatch: ToolItem[] = [];
  const flushRO = () => {
    if (roBatch.length === 0) return;
    body.push(<ReadOnlyBatch key={`rob-${roBatch[0].id}`} items={[...roBatch]} subcalls={subcalls} />);
    roBatch.length = 0;
  };
  for (const it of displayItems) {
    if (it.kind === "tool" && !it.parentId && it.name !== "todo_write" && it.name !== "exit_plan_mode" && it.status !== "running" && isReadOnlyTool(it.name)) {
      roBatch.push(it as ToolItem);
      continue;
    }
    flushRO();
    switch (it.kind) {
      case "tool":
        if (it.parentId) break;
        if (it.name === "todo_write") break;
        if (it.name === "exit_plan_mode") break;
        body.push(<ToolCard key={it.id} item={it as ToolItem} subcalls={subcalls.get(it.id)} />);
        break;
      case "phase": body.push(<PhaseCard key={it.id} text={it.text} />); break;
      case "assistant": {
        const displayItem = it;
        body.push(<AssistantMessage key={it.id} item={displayItem as AssistantItem} />);
        break;
      }
    }
  }
  flushRO();

  return (
    <div className={`turn-collapse${open ? " turn-collapse--open" : ""}`} data-entrance={displayItems[0]?.id || undefined}>
      <button
        type="button"
        className="reasoning__head"
        onClick={() => setOpen((v) => !v)}
        aria-expanded={open}
      >
        <ChevronRight className={`reasoning__chevron${open ? " reasoning__chevron--open" : ""}`} size={12} />
        <span className="turn-collapse__label">{label}</span>
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
type NoticeItem = Extract<Item, { kind: "notice" }>;

function PhaseCard({ text }: { text: string }) {
  return <div className="phase" data-entrance="true"><ProcessPhaseIcon size={12} /><span>{text}</span></div>;
}

function NoticeCard({ level, text }: { level: NoticeItem["level"]; text: string }) {
  return (
    <div className={`notice-line notice-line--${level}`} data-entrance="true">
      <span className="notice-line__icon">{level === "warn" ? "⚠ " : "ℹ "}</span>
      <span className="notice-line__text">{text}</span>
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
