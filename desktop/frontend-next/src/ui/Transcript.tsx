import { OriginLine } from "./team/OriginLine";
import { Fragment, memo, useCallback, useContext, useEffect, useId, useLayoutEffect, useMemo, useRef, useState, type Dispatch, type ReactNode, type RefObject, type SetStateAction } from "react";
import { t } from "../i18n";
import type { Item, Waiting } from "../state/session";
import type { ExtensionSurface } from "../port/wire";
import type { AgentPort, ApprovalVerdict, Checkpoint, RewindPlan, RewindResult, RewindScope } from "../port/port";
import { Hero } from "./Hero";
import { Await } from "./Await";
import { ToolCard } from "./cards/ToolCard";
import { Boundary } from "./Boundary";
import { LazyChart } from "./chart/LazyChart";
import { chartOfTool } from "./chart/spec";
import { GuardianCard } from "./cards/GuardianCard";
import { ApprovalCard, type PlanAction } from "./cards/ApprovalCard";
import { AskCard } from "./cards/AskCard";
import { revealNested } from "./cards/nestreveal";
import { SurfaceCard, surfaceTool } from "./surface/SurfaceCard";
import { ElicitCard } from "./cards/ElicitCard";
import { SayCard, type ReplyActions } from "./cards/SayCard";
import { DeliveryCard } from "./document/DeliveryCard";
import { deliveriesByReply } from "./document/deliveries";
import { CompactionCard } from "./cards/CompactionCard";
import { WorkedLine } from "./cards/WorkedLine";
import { ReadsCard } from "./cards/ReadsCard";
import { UserCard } from "./cards/UserCard";
import { NoticeCard } from "./cards/NoticeCard";
import { RememberCard } from "./cards/RememberCard";
import { ExtensionCard } from "./cards/ExtensionCard";
import { toolChangedFile, toolFailed } from "./cards/outcome";
import { NewAnswerLine, useNewAnswerAnchor } from "./NewAnswerLine";
import { drawn, foldStats, repeatedSpeakers, sameMembers, transcriptRows, type Shown } from "./turnrows";
import { BroughtResults } from "./BroughtResults";
import { ReceiptSlot } from "./ReceiptSlot";
import { WorkFold } from "./WorkFold";
import { opensTurn, useBlocks, useBlockKeys } from "./blocks";
import { useHistoryAnchor, useOlderPageIntent } from "./useHistoryAnchor";
import { HistoryPageControl } from "./HistoryPageControl";
import type { HistoryPaging } from "./useHistoryPages";
import { Rail, type RailMark } from "./Rail";
import { railMarks } from "./railmarks";
import { StudioIcon } from "./StudioIcon";
import { LiveWork, useStartsOpen } from "../state/foldpref";
import { landingBox, renderedBox, useFindLanding, useFindPaint } from "./findland";
import { ScrollJump } from "./ScrollJump";
import { landing as landingOf, type Direction } from "./jumpreach";
import { chunkText, heldBlocks, type Held } from "./chunk_text";

interface Props {
  historyPaging?: HistoryPaging;
  items: Item[];
  port?: Pick<AgentPort, "workspaceImageURL"> & Partial<Pick<AgentPort, "surfaceAction" | "surfaceActionState" | "cancelQueued" | "engageQuestion" | "stopToolCall" | "refinePrompt" | "toolOutput">>;
  revision: number;
  waiting: Waiting;
  scroll: RefObject<HTMLDivElement | null>;
  hidden: boolean;
  // A request from outside to show one tool call — the run graph asking for the
  // node you clicked. The nonce, not the id, is what makes asking twice land
  // twice; null while nothing has been asked for.
  focus: { call: string; n: number } | null;
  find?: { id: string; nth: number; n: number; mode?: { regex?: boolean; caseSensitive?: boolean } } | null;
  query?: string;
  onApprove: (itemId: string, id: string, v: ApprovalVerdict) => Promise<void>;
  onFullAccess: (itemId: string) => Promise<void>;
  onPlan: (itemId: string, id: string, action: PlanAction) => Promise<void>;
  onAnswer: (itemId: string, id: string, answers: { questionId: string; selected: string[]; value?: Record<string, unknown> }[]) => Promise<void>;
  onForget: (itemId: string, name: string) => void;
  onSettings?: (at?: string) => void;
  // Takes a still-queued line back. Only rows the kernel has given an
  // item id can offer it, and only until the turn reads them.
  onExtInvoke: (name: string) => void;
  // Views published against a tool call, keyed by anchor. A card looks itself
  // up here rather than being handed one, so an arriving takeover repaints the
  // one card it names and nothing else.
  takeovers?: Record<string, ExtensionSurface>;
  // What a finished reply can be acted on with. One object rather than five
  // props: they are one capability set and travel together.
  reply?: ReplyActions;
  // Rewriting a message: the turn it takes back, and what to send instead.
  onResend?: (turn: number, text: string) => Promise<void>;
  onExtSubmit: (pluginId: string, surfaceId: string, values: Record<string, unknown>) => void | Promise<void>;
  // The checkpoint each user card can return to, keyed by item id. Absent for a
  // card whose turn could not be matched — see state/checkpoints.
  needsProject: boolean;
  onOpenProject: () => void;
  onKeepHere: () => void;
  // What an empty conversation shows in place of the window's own hero.
  hero?: ReactNode;
  foldWork?: boolean;
  turnRunning?: boolean;
  scope?: string;
  checkpoints: Map<string, Checkpoint>;
  onPrepareRewind: (turn: number, scope: RewindScope) => Promise<RewindPlan>;
  onPrepareFileRevert: (path: string) => Promise<RewindPlan>;
  onCommitFileRevert: (planId: string, resolution?: string) => Promise<RewindResult>;
  onCommitRewind: (planId: string, text?: string) => Promise<RewindResult>;
  /** Cards that have not had their one entrance yet. Owed by the projection,
   *  spent by the first render that draws them — never by the animation, which
   *  may not run at all. */
  entering: string[];
  onEntered: (ids: string[]) => void;
  /** Calls the reader asked to see again at the end, in the order asked. Absent
   *  where no live pane stands behind the transcript: no card offers the action. */
  brought?: { ids: string[]; bring: (id: string) => void; dismiss: (id: string) => void };
}

export function Transcript({ items, port, historyPaging, entering, onEntered, brought, revision, waiting, scroll, hidden, focus, find, query, onApprove, onFullAccess, onPlan, onAnswer, onForget, onSettings, onExtInvoke, onExtSubmit, reply, onResend, takeovers = {}, checkpoints, onPrepareRewind, onCommitRewind, onPrepareFileRevert, onCommitFileRevert, needsProject, onOpenProject, onKeepHere, hero, foldWork = false, turnRunning = false, scope = "" }: Props) {
  // A block the selection touches must not leave the DOM. Unmounting the node a
  // selection is anchored to makes the browser remap that selection onto
  // whatever is still mounted — which reads as "I selected up there and the
  // bottom got selected too". A block the selection merely passes through, with
  // neither end inside it, is held as plain text from the data: mounting its
  // cards in one synchronous render is what made a long drag freeze.
  const [held, setHeld] = useState<Map<number, Held>>(new Map());

  // Stick to the bottom only while the reader is already there; scrolling up
  // must not be yanked back by incoming frames.
  const [pinned, setPinned] = useState(true);
  const end = useRef<HTMLDivElement>(null);
  const flow = useRef<HTMLDivElement>(null);
  const scrollId = useId();
  // Read from observer callbacks that must not be torn down and rebuilt every
  // time the reader crosses the bottom.
  const at = useRef(pinned);
  at.current = pinned;

  // Set while a follow is scrolling, so the resize it causes is not read as new
  // content arriving.
  const self = useRef(false);
  // When the reader last touched an input device. Coming back to the bottom
  // only resumes the follow if they are the one who went there.
  const gesture = useRef(0);
  const wasAt = useRef(0);
  // Scroll events before this moment are the browser's, not the reader's.
  const quiet = useRef(0);
  const landing = useRef(0);

  // Following must settle inside the layout pass. scrollIntoView also moves
  // ancestor scrollers, so this owner writes only its own viewport.
  const follow = useCallback(() => {
    const root = scroll.current;
    if (!root) return;
    self.current = true;
    root.scrollTop = root.scrollHeight;
    requestAnimationFrame(() => {
      self.current = false;
    });
  }, [scroll]);

  useFindPaint(flow, hidden, query ?? "", find?.id ?? null, find?.nth ?? 0, find?.mode);

  useEffect(() => {
    const onSelect = () => {
      const sel = document.getSelection();
      const root = scroll.current;
      if (!root || !sel || sel.isCollapsed || sel.rangeCount === 0) {
        setHeld((prev) => (prev.size ? new Map() : prev));
        return;
      }
      const range = sel.getRangeAt(0);
      if (!root.contains(range.commonAncestorContainer)) return;
      const next = heldBlocks(range, root.querySelectorAll(".chunk"));
      setHeld((prev) => (prev.size === next.size && [...next].every(([i, how]) => prev.get(i) === how) ? prev : next));
    };
    document.addEventListener("selectionchange", onSelect);
    return () => document.removeEventListener("selectionchange", onSelect);
  }, [scroll]);

  // Following is about intent. Two things carry it, and neither alone is
  // enough. The input devices are unambiguous — a wheel, a key, a drag on the
  // scrollbar is the reader, always — and they release immediately, without
  // waiting for a position test that virtualisation makes unreliable. But a
  // scroll can also arrive with no gesture at all (a screen reader moving the
  // cursor, the browser's own find), and for those the position is the only
  // evidence there is: an upward move that leaves the bottom behind is the
  // reader, one that stays inside the margin is the transcript growing.
  // Zero, not now: this stamp is what lets the bottom marker resume the follow,
  // and a gesture that just left the bottom must not also license going back to
  // it. Only a downward one does.
  const release = useCallback(() => {
    landing.current++;
    gesture.current = 0;
    if (!at.current) return;
    at.current = false;
    setPinned(false);
  }, []);

  useEffect(() => {
    const root = scroll.current;
    if (!root) return;
    const mark = () => {
      landing.current++;
      gesture.current = performance.now();
    };
    const onWheel = (e: WheelEvent) => (e.deltaY < 0 ? release() : mark());
    const onKey = (e: KeyboardEvent) => {
      if (["ArrowUp", "PageUp", "Home"].includes(e.key)) release();
      else if (["ArrowDown", "PageDown", "End", " "].includes(e.key)) mark();
    };
    // Reading scrollTop is free; scrollHeight and clientHeight force a layout,
    // which is why they are read only here — while following, on an upward
    // event — and never per frame.
    const onScroll = () => {
      const top = root.scrollTop;
      const up = top < wasAt.current - 2;
      wasAt.current = top;
      const away = root.scrollHeight - top - root.clientHeight;
      if (!up || self.current || !at.current || performance.now() < quiet.current) return;
      if (away <= 48) return;
      release();
    };
    root.addEventListener("wheel", onWheel, { passive: true });
    root.addEventListener("keydown", onKey);
    root.addEventListener("pointerdown", mark);
    root.addEventListener("touchmove", mark, { passive: true });
    root.addEventListener("scroll", onScroll, { passive: true });
    return () => {
      root.removeEventListener("wheel", onWheel);
      root.removeEventListener("keydown", onKey);
      root.removeEventListener("pointerdown", mark);
      root.removeEventListener("touchmove", mark);
      root.removeEventListener("scroll", onScroll);
    };
  }, [scroll, release]);

  // One observer for every block, not one per block. Hundreds of separate
  // observers did not reliably report a block leaving — blocks stayed mounted
  // 400,000px above the viewport — and each one is per-frame work of its own.
  // A single observer watching many targets is what the API is for.
  const watchers = useRef(new Map<Element, (near: boolean) => void>());
  const lens = useRef<IntersectionObserver | null>(null);

  // Not while hidden: nothing intersects a display:none root, so the observer
  // would report every block as far away and unmount the entire transcript.
  // Coming back would then have to rebuild it from estimated heights under a
  // scroll position that no longer means anything. A pane on another tab keeps
  // what it had — that is the whole point of leaving it mounted.
  useEffect(() => {
    const root = scroll.current;
    if (!root || hidden) return;
    // Two screens of margin: a block is mounted well before it can be scrolled
    // to, so reaching it never waits on a render.
    const obs = new IntersectionObserver(
      (entries) => {
        for (const e of entries) watchers.current.get(e.target)?.(e.isIntersecting);
      },
      { root, rootMargin: "200% 0px" },
    );
    lens.current = obs;
    for (const el of watchers.current.keys()) obs.observe(el);
    return () => {
      obs.disconnect();
      lens.current = null;
    };
  }, [scroll, hidden]);

  // Stable, so a block subscribes once for its lifetime.
  const watch = useCallback((el: Element, cb: (near: boolean) => void) => {
    watchers.current.set(el, cb);
    lens.current?.observe(el);
    return () => {
      watchers.current.delete(el);
      lens.current?.unobserve(el);
    };
  }, []);

  // Coming back to the bottom resumes it. Watching a marker at the end of the
  // flow answers that without measuring the scroller, which following a live
  // answer would otherwise do on every frame.
  useEffect(() => {
    const marker = end.current;
    const root = scroll.current;
    if (!marker || !root || hidden) return;
    const io = new IntersectionObserver(
      ([e]) => {
        if (!e.isIntersecting) return;
        // The marker crossing back into view is not by itself a reason to
        // resume: blocks mounting and unmounting move it past this edge on
        // their own, and every one of those was yanking the reader back down.
        // Only a hand on the wheel counts, and only just now.
        if (performance.now() - gesture.current > 400 && performance.now() > quiet.current) return;
        at.current = true;
        setPinned(true);
      },
      { root, rootMargin: "0px 0px 48px 0px" },
    );
    io.observe(marker);
    return () => io.disconnect();
  }, [scroll, hidden]);

  // A hidden container scrolls nothing: scrollIntoView is a no-op there, and
  // every block unmounts because nothing intersects a display:none root. So
  // coming back has to do two things. Restore the follow — otherwise the
  // transcript sits where you left it, thousands of pixels above what arrived
  // meanwhile. And ignore the scroll events of the next few frames: putting
  // scrollTop back and remounting the blocks both move it, and reading either
  // as "the reader scrolled up" is what silently ended the follow.
  useEffect(() => {
    if (hidden) return;
    quiet.current = performance.now() + 400;
    if (at.current) follow();
  }, [hidden, follow]);

  // Before paint: the reader must never see a frame where new text landed and
  // the view had not caught up.
  useLayoutEffect(() => {
    if (pinned) follow();
  }, [items, pinned, follow]);

  // A mark's message may be inside an unmounted block, which has no node to
  // scroll to. Land on the block first — that always exists, holding its own
  // height — and the observer mounts it because it is now in range; the second
  // pass then puts the message itself under the reader. Marked as a gesture,
  // or the follow would read this scroll as the transcript moving and stay
  // pinned to the bottom the reader just left.
  const land = useCallback(
    (block: number, into: number, selector: string, clear = 12, then?: (found: HTMLElement, root: HTMLElement) => boolean, highlight = true) => {
      const root = scroll.current;
      const inner = flow.current;
      if (!root || !inner) return;
      // A newer landing owns the scroll; an older one still settling stops.
      const mine = ++landing.current;
      gesture.current = 0;
      at.current = false;
      setPinned(false);
      // Measured against the scroller, not offsetTop: the nearest positioned
      // ancestor is .pane, which does not scroll, so offsetTop answers "where
      // in the window" — writing that back as scrollTop barely moves anything.
      const topOf = (el: HTMLElement) => el.getBoundingClientRect().top - root.getBoundingClientRect().top + root.scrollTop;
      // A caller that can place a mounted target itself spares the row-top hop.
      const ready = inner.querySelector<HTMLElement>(selector);
      if (ready && then?.(ready, root)) return;
      const chunk = inner.querySelectorAll<HTMLElement>(".chunk")[block];
      if (ready) root.scrollTop = topOf(renderedBox(ready)) - clear;
      else if (chunk) root.scrollTop = topOf(chunk) + into * chunk.offsetHeight - clear;
      const settle = (tries: number, last = NaN) => {
        if (mine !== landing.current || !inner.isConnected) return;
        const found = inner.querySelector<HTMLElement>(selector);
        if (found) {
          const el = landingBox(found);
          const top = topOf(el) - clear;
          root.scrollTop = top;
          // Blocks above it mount with their real height as it comes into view,
          // which moves it; landing holds until two frames agree.
          if (tries > 0 && !(Math.abs(top - last) <= 2)) {
            requestAnimationFrame(() => settle(tries - 1, top));
            return;
          }
          if (highlight) { el.setAttribute("data-hit", ""); setTimeout(() => el.removeAttribute("data-hit"), 1200); }
          then?.(found, root);
          return;
        }
        if (tries > 0) requestAnimationFrame(() => settle(tries - 1));
      };
      requestAnimationFrame(() => settle(20));
    },
    [scroll, flow],
  );

  const jumpTo = useCallback(
    (mark: RailMark) => land(mark.block, mark.of > 1 ? mark.within / mark.of : 0, `[data-item="${CSS.escape(mark.id)}"]`),
    [land],
  );

  // "Back to latest" cannot just write scrollTop once: where the bottom is
  // changes as blocks mount under it. Turning the follow back on lets the same
  // correction that tracks a live answer carry it the rest of the way.
  const resume = useCallback(() => {
    landing.current++;
    at.current = true;
    setPinned(true);
    follow();
  }, [follow]);

  // The button goes to the end it names. Held with Shift it steps to the nearest
  // expanded call or thought in that direction instead, else the end: a jump
  // that should not cross a whole stretch of context the reader has opened.
  const jumpToward = useCallback(
    (dir: Direction, step: boolean) => {
      const root = scroll.current;
      if (!root) return;
      if (!step) {
        if (dir === "down") return resume();
        landing.current++;
        release();
        root.scrollTop = 0;
        return;
      }
      const base = root.getBoundingClientRect().top - root.scrollTop;
      const tops = [...root.querySelectorAll<HTMLElement>("details.tool-disclosure[open], details.think[open]")].map((el) => el.getBoundingClientRect().top - base);
      const to = landingOf(dir, tops, root.scrollTop, root.clientHeight, root.scrollHeight);
      if (to === "end") return resume();
      landing.current++;
      release();
      root.scrollTop = to;
    },
    [scroll, release, resume],
  );

  // Blocks mounting and unmounting move the bottom out from under a follow that
  // only fires on events — which left the viewport stranded half a transcript
  // up. Watching the height itself covers every cause: a block loading, a card
  // expanding, a webfont finally arriving.
  useEffect(() => {
    const el = flow.current;
    if (!el) return;
    const ro = new ResizeObserver(() => {
      if (at.current) follow();
    });
    ro.observe(el);
    return () => ro.disconnect();
  }, [follow]);

  let tail = items.length - 1;
  while (tail >= 0) {
    const item = items[tail];
    if (item.t !== "user" || !item.pending) break;
    tail--;
  }
  const last = items[tail];
  const live = last?.t === "say" && !last.done ? last : undefined;
  const blocks = useBlocks(items, live ? tail : items.length, revision);
  const blockKeys = useBlockKeys(blocks);
  const loadOlder = useHistoryAnchor(historyPaging, scroll, blocks, hidden, landing, release, land);
  useOlderPageIntent({ scroll, hidden, load: loadOlder, generation: landing });

  useFindLanding(find, query ?? "", hidden, blocks, live, land);

  // Which block holds a given call, so the graph can land on one the way the
  // rail lands on a message: the card may sit in a block that is not mounted
  // and therefore has no node to scroll to yet.
  const callBlock = useMemo(() => {
    const at = new Map<string, { block: number; into: number }>();
    blocks.forEach((block, b) =>
      block.forEach((it, i) => {
        if (it.t !== "tool") return;
        const into = block.length > 1 ? i / block.length : 0;
        for (const id of [it.tool.id, ...it.children.map((c) => c.id)]) if (id) at.set(id, { block: b, into });
      }),
    );
    return at;
  }, [blocks]);

  // A hidden pane has no geometry: every rect reads zero and the scroll lands
  // nowhere. So the request is held, not spent, until this tab is the one on
  // screen — which is also what makes the caller's ordering not matter.
  const landed = useRef(0);
  useEffect(() => {
    if (hidden || !focus || landed.current === focus.n) return;
    landed.current = focus.n;
    const where = callBlock.get(focus.call);
    revealNested(focus.call);
    if (where) land(where.block, where.into, `[data-call="${CSS.escape(focus.call)}"]`);
  }, [hidden, focus, callBlock, land]);

  // Drawn is spent. Not "the animation finished" — reduced motion runs none,
  // and a card can leave the document before one ends, either of which would
  // leave the debt standing and let the fact arrive a second time later.
  useEffect(() => {
    if (entering.length > 0) onEntered(entering);
  }, [entering, onEntered]);
  const owed = useMemo(() => new Set(entering), [entering]);
  const kept = useRef<Set<string>>(new Set());
  const echoes = useMemo(() => {
    const next = repeatedSpeakers(items);
    if (!sameMembers(kept.current, next)) kept.current = next;
    return kept.current;
  }, [items]);

  const bring = brought?.bring;
  const onBring = useMemo(() => (bring ? (id: string) => { bring(id); resume(); } : undefined), [bring, resume]);
  const rowProps = { onBring, owed, echoes, port, onApprove, onFullAccess, onPlan, onAnswer, onForget, onSettings, onExtInvoke, takeovers, onExtSubmit, onPrepareRewind, onCommitRewind, onPrepareFileRevert, onCommitFileRevert, reply, onResend };

  const marks = railMarks(blocks, (id) => checkpoints.get(id)?.files ?? 0);

  return (
    <div
      className="scroll"
      id={scrollId}
      data-pane="flow"
      ref={scroll}
      hidden={hidden}
    >
      <Rail marks={marks} total={items.length} scroll={scroll} flow={flow} onJump={jumpTo} bound={!hidden} scope={scope} earlier={historyPaging?.earlier ?? 0} onLoadEarlier={historyPaging?.hasMore ? loadOlder : undefined} />
      <div className="flow-edge" aria-hidden="true" />
      {/* 空转录是「窗口的空白」，壁纸该在那儿；一有内容它就是内容了。 */}
      <div className="flow" ref={flow} data-empty={items.length === 0 && !historyPaging?.hasMore && !historyPaging?.busy && !historyPaging?.failed ? "" : undefined}>
        <HistoryPageControl paging={historyPaging} load={loadOlder} />
        {items.length === 0 && !historyPaging?.hasMore && !historyPaging?.busy && !historyPaging?.failed && (
          hero ?? <Hero needsProject={needsProject} onOpen={onOpenProject} onKeep={onKeepHere} />
        )}
        {blocks.map((block, i) => (
          <Block
            key={blockKeys[i]}
            items={block}
            checkpoints={checkpoints}
            watch={watch}
            // The block a new card lands in has no measured height yet, so it
            // mounts without waiting for the observer's first callback.
            eager={i === blocks.length - 1}
            keep={held.get(i)}
            foldWork={foldWork}
            tail={turnRunning && i === blocks.length - 1}
            {...rowProps}
          />
        ))}
        {live && <Row it={live} {...rowProps} cp={checkpoints.get(live.id)} />}
        {brought && <BroughtResults items={items} ids={brought.ids} onDismiss={brought.dismiss} takeovers={takeovers} onExtInvoke={onExtInvoke} onPrepareFileRevert={onPrepareFileRevert} onCommitFileRevert={onCommitFileRevert} />}
        {waiting.ttftSince && <Await since={waiting.ttftSince} retry={waiting.retry} />}
        <div ref={end} className="flow-end" aria-hidden="true" />
      </div>
      <ScrollJump scroll={scroll} flow={flow} active={!hidden} onJump={jumpToward} />
    </div>
  );
}

// Off-screen cards leave the DOM entirely, holding their place with the height
// they last measured. content-visibility already spared them layout and paint;
// this is about the cost of existing at all — at 4000 turns the nodes alone put
// 300k of them and 111MB behind a window nobody can see through.
const Block = memo(function Block({
  items,
  checkpoints,
  watch,
  eager,
  keep,
  foldWork,
  tail,
  ...rowProps
}: {
  items: Item[];
  checkpoints: Map<string, Checkpoint>;
  watch: (el: Element, cb: (near: boolean) => void) => () => void;
  eager: boolean;
  // The selection reaches into this block, so it stays in the DOM however far
  // off-screen it scrolls: whole when a selection end is inside it, as plain
  // text when the selection only passes through.
  keep: Held | undefined;
  // Whether a settled turn's work is folded under one line (the kernel's [ui] fold_turn_work).
  foldWork: boolean;
  // This block holds the end of a turn that is still being worked on, which stays open.
  tail: boolean;
} & RowHandlers) {
  const box = useRef<HTMLDivElement>(null);
  const [near, setNear] = useState(eager);
  const tall = useRef(0);

  useEffect(() => (box.current ? watch(box.current, setNear) : undefined), [watch]);

  // Measured while mounted so the placeholder that replaces it is exactly as
  // tall — otherwise unmounting above the viewport would jerk the scroll.
  useLayoutEffect(() => {
    if (near && box.current) tall.current = box.current.offsetHeight;
  });

  // Tool traffic is evidence for the answer, not a stack of equally important
  // messages; transcriptRows folds it and keeps each sentence above its own.
  const rows = transcriptRows(items, foldWork ? { running: tail } : undefined);
  const newAnswerAt = useNewAnswerAnchor();
  const deliveries = useMemo(() => deliveriesByReply(items), [items]);
  const delivery = (id: string) => deliveries.has(id) ? <DeliveryCard files={deliveries.get(id)!} reply={rowProps.reply} /> : null;
  // Which runs of work are open, held here so re-hosting one under its sentence
  // does not close it.
  const [opened, setOpened] = useState<Record<string, boolean>>({});
  const drawRows = (list: Shown[]): ReactNode[] => list.map((row) => {
    if ("fold" in row) {
      const key = `fold:${row.id}`;
      const stats = foldStats(row.fold, (item) => (item.t === "tool" ? Number(toolFailed(item.tool)) : item.t === "reads" ? item.tools.filter(toolFailed).length : 0));
      return (
        <WorkFold key={key} open={opened[key] ?? false} onOpen={(next) => setOpened((all) => (all[key] === next ? all : { ...all, [key]: next }))} {...stats}>
          {drawRows(row.fold)}
        </WorkFold>
      );
    }
    return "item" in row ? (
      <Fragment key={row.item.id}>
        {row.item.id === newAnswerAt && <NewAnswerLine />}
        {opensTurn(row.item) && row.item.id !== newAnswerAt && <hr className="turn-rule" />}
        <Row
          it={row.item}
          {...rowProps}
          cp={checkpoints.get(row.item.id)}
          settled={!row.activity?.some((item) => item.t === "tool" && item.running)}
          afterAnswer={row.activity || deliveries.has(row.item.id) ? <>{delivery(row.item.id)}{row.activity && <ActivityGroup items={row.activity} checkpoints={checkpoints} opened={opened} onOpened={setOpened} {...rowProps} />}</> : undefined}
        />
      </Fragment>
    ) : (
      <Fragment key={`activity:${row.activity[0].id}`}>
        {row.activity.map((item) => <Fragment key={item.id}>{delivery(item.id)}</Fragment>)}
        <ActivityGroup items={row.activity} checkpoints={checkpoints} opened={opened} onOpened={setOpened} {...rowProps} />
      </Fragment>
    );
  });

  return (
    <div
      className="chunk"
      ref={box}
      style={near || keep === "full" ? undefined : { height: `${tall.current || items.length * 96}px`, overflow: keep ? "hidden" : undefined }}
    >
      {near || keep === "full" ? drawRows(rows) : keep === "text" && chunkText(items)}
    </div>
  );
});

interface RowHandlers {
  onBring?: (id: string) => void;
  owed: Set<string>;
  echoes: Set<string>;
  port: Props["port"];
  onApprove: Props["onApprove"];
  onFullAccess: Props["onFullAccess"];
  onPlan: Props["onPlan"];
  onAnswer: Props["onAnswer"];
  onForget: Props["onForget"];
  onSettings: Props["onSettings"];
  onExtInvoke: Props["onExtInvoke"];
  takeovers: Record<string, ExtensionSurface>;
  onExtSubmit: Props["onExtSubmit"];
  onPrepareRewind: Props["onPrepareRewind"];
  onCommitRewind: Props["onCommitRewind"];
  onPrepareFileRevert: Props["onPrepareFileRevert"];
  reply: Props["reply"];
  onResend: Props["onResend"];
  onCommitFileRevert: Props["onCommitFileRevert"];
}

const ActivityGroup = memo(function ActivityGroup({
  items,
  checkpoints,
  opened,
  onOpened,
  ...rowProps
}: {
  items: Item[];
  checkpoints: Map<string, Checkpoint>;
  opened: Record<string, boolean>;
  onOpened: Dispatch<SetStateAction<Record<string, boolean>>>;
} & RowHandlers) {
  const runs = items.filter((item) => item.t === "tool" && item.running).length;
  const running = runs > 0;
  // Whether this group is open is held above it, because the group itself does
  // not survive the turn: a run of work starts as a row of its own and is
  // re-hosted under the sentence it belongs to once that sentence arrives,
  // which unmounts it. State kept inside would be lost at exactly that moment.
  const gid = items[0]?.id ?? "";
  const live = useContext(LiveWork).has(gid);
  const changed = items.some((item) => item.t === "tool" && toolChangedFile(item.tool));
  const start = useStartsOpen("activity", running || live, false, changed);
  const open = opened[gid] ?? start;
  const setOpen = useCallback(
    (next: boolean) => onOpened((all) => (all[gid] === next ? all : { ...all, [gid]: next })),
    [gid, onOpened],
  );
  const calls = items.reduce((count, item) => count + (item.t === "reads" ? item.tools.length : 1), 0);
  const failures = items.reduce((count, item) => {
    if (item.t === "tool") return count + (toolFailed(item.tool) ? 1 : 0);
    if (item.t === "reads") return count + item.tools.filter(toolFailed).length;
    return count;
  }, 0);
  return (
    <details className="activity-group" data-failed={failures ? "" : undefined} data-running={running ? "" : undefined} open={open} onToggle={(event) => event.currentTarget.open !== open && setOpen(event.currentTarget.open)}>
      <summary>
        <StudioIcon name={running ? "clock" : failures ? "warning" : "check"} className="activity-status-icon" />
        <span className="activity-title">{t("执行过程")}</span>
        {running && <span className="activity-running">{t("{n} 项运行中", { n: runs })}</span>}
        {failures > 0 && <span className="activity-errors">{t("{n} 项失败", { n: failures })}</span>}
        <span className="activity-count">{t("{count} 项操作", { count: calls })}</span>
        <StudioIcon name="down" className="activity-fold-icon" />
      </summary>
      <div className="activity-body">
        {items.map((item) => <Row key={item.id} it={item} activity {...rowProps} cp={checkpoints.get(item.id)} />)}
      </div>
      <p className="activity-note">{t("点击步骤可原位查看详情、结果或文件差异")}</p>
    </details>
  );
});

// A streamed token replaces one item and leaves the rest identical, so the rest
// must not re-render: at a working session's length that walk cost more per
// chunk than parsing the message did.
const Row = memo(function Row({
  it,
  port,
  onBring,
  owed,
  echoes,
  onApprove,
  onFullAccess,
  onPlan,
  onForget,
  onSettings,
  onAnswer,
  onExtInvoke,
  reply,
  onResend,
  takeovers,
  onExtSubmit,
  cp,
  onPrepareRewind,
  onCommitRewind,
  onPrepareFileRevert,
  onCommitFileRevert,
  afterAnswer,
  settled,
  activity = false,
}: RowHandlers & { it: Item; cp?: Checkpoint; afterAnswer?: ReactNode; settled?: boolean; activity?: boolean }) {
  // Read once, for the life of this element. The projection spends the debt as
  // soon as this is drawn, and a card whose answer is still streaming would
  // otherwise lose the attribute mid-animation and have it cut short. A block
  // that scrolls back mounts a new Row, which asks again and is told no.
  const enter = useRef(owed.has(it.id)).current;
  // display:contents, so this frame carries the one-shot mark and the card
  // stays exactly the child of .chunk that its layout is written against.
  return (
    <div className="enterbox" data-item={it.id} data-enter={enter ? "" : undefined}>
      {(() => {
  switch (it.t) {
    case "user":
      return (
        <UserCard
          item={it}
          port={port}
          cp={cp}
          onResend={onResend}
          onPrepareRewind={onPrepareRewind}
          onCommitRewind={onCommitRewind}
        />
      );
    case "say":
      // Reasoning arrives long before the first answer token on a thinking
      // model. Gating the card on text meant all of it stayed invisible and
      // then landed at once. An empty card is still not a message, so a turn
      // that produced neither draws nothing.
      return drawn(it) || afterAnswer ? <SayCard item={it} afterAnswer={afterAnswer} settled={settled} reply={reply} quiet={echoes.has(it.id)} /> : null;
    case "tool":
      // The ask tool also raises ask_request, which carries the id /answer
      // needs. Drawing the tool call too put two copies of the same question on
      // screen, each answerable.
      if (it.tool.name === "ask") return null;
      { const surface = surfaceTool(it.tool, it.running, port); if (surface !== undefined) return surface; }
      return (() => {
        const card = (
          <ToolCard
            tool={it.tool}
            running={it.running}
            activity={activity}
            children={it.children}
            readOutput={port?.toolOutput?.bind(port)}
            omitted={it.omitted}
            takeover={it.tool.id ? takeovers[`tool:${it.tool.id}`] : undefined}
            onExtInvoke={onExtInvoke}
            onPrepareFileRevert={onPrepareFileRevert}
            onCommitFileRevert={onCommitFileRevert}
            onBring={it.tool.id && onBring ? () => onBring(it.tool.id!) : undefined}
            onStop={port?.stopToolCall && it.tool.id ? () => void port.stopToolCall!(it.tool.id!).catch(() => {}) : undefined}
          />
        );
        const chart = activity ? null : chartOfTool(it.tool);
        return chart ? (
          <Boundary fallback={card} retryKey={it.tool.args}>
            <LazyChart spec={chart} callId={it.tool.id} />
          </Boundary>
        ) : card;
      })();
    case "reads":
      return <ReadsCard tools={it.tools} />;
    case "guardian":
      return <GuardianCard g={it.g} />;
    case "approval":
      return <ApprovalCard item={it} onApprove={onApprove} onFullAccess={onFullAccess} onPlan={onPlan} />;
    case "ask":
      return it.ask.questions.some(q => q.surface) ? <SurfaceCard item={it} onAnswer={onAnswer} port={port} /> : it.ask.origin ? <ElicitCard item={it} onAnswer={onAnswer} /> : <AskCard item={it} onAnswer={onAnswer} onEngage={port?.engageQuestion?.bind(port)} refine={port?.refinePrompt?.bind(port)} />;
    case "compaction":
      return <CompactionCard c={it.c} done={it.done} />;

    case "remember":
      return <RememberCard m={it.m} forgotten={it.forgotten} onForget={(name) => onForget(it.id, name)} />;
    case "receipt":
      return <ReceiptSlot r={it.r} />;
    case "worked":
      return <WorkedLine ms={it.ms} startedAt={it.startedAt} />;
    case "extension":
      return <ExtensionCard ext={it.ext} onInvoke={onExtInvoke} onSubmit={onExtSubmit} />;
    case "notice":
      return <NoticeCard item={it} onSettings={onSettings} />;
    case "origin":
      return <OriginLine origin={it.origin} text={it.text} />;
  }
      })()}
    </div>
  );
});
