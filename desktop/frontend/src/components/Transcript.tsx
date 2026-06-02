import { useEffect, useRef, useState } from "react";
import type { Item } from "../lib/useController";
import { AssistantMessage, UserMessage } from "./Message";
import { ToolCard } from "./ToolCard";
import { Welcome } from "./Welcome";

type ToolItem = Extract<Item, { kind: "tool" }>;

function scrollVersion(items: Item[]): string {
  return items
    .map((it) => {
      switch (it.kind) {
        case "assistant":
          return `${it.id}:a:${it.text.length}:${it.reasoning.length}:${it.streaming ? 1 : 0}`;
        case "tool":
          return `${it.id}:t:${it.name}:${it.status}:${it.args.length}:${it.output?.length ?? 0}:${it.error?.length ?? 0}:${it.truncated ? 1 : 0}`;
        default:
          return `${it.id}:${it.kind}`;
      }
    })
    .join("|");
}

function repinIfWasPinned(
  el: HTMLDivElement,
  stick: { current: boolean },
  frame: { current: number | null },
  containerHeightDelta: number,
) {
  const bottomDistance = el.scrollHeight - el.scrollTop - el.clientHeight;
  // + delta reconstructs the bottom distance from before the height changed
  if (!stick.current && bottomDistance + containerHeightDelta >= 80) return;
  stick.current = true;
  if (frame.current !== null) cancelAnimationFrame(frame.current);
  frame.current = requestAnimationFrame(() => {
    if (stick.current) el.scrollTop = el.scrollHeight;
    frame.current = null;
  });
}

export function Transcript({
  items,
  footerHeight = 0,
  onPrompt,
  onRewind,
}: {
  items: Item[];
  footerHeight?: number;
  onPrompt: (text: string) => void;
  onRewind?: (turn: number, scope: string) => void;
}) {
  const scrollRef = useRef<HTMLDivElement>(null);
  // stick tracks whether the view is pinned to the bottom; once the user scrolls
  // up to read, we stop yanking them back down.
  const stick = useRef(true);
  const resizeFrame = useRef<number | null>(null);
  const lastClientHeight = useRef<number | null>(null);
  const lastFooterHeight = useRef<number | null>(null);

  const onScroll = () => {
    const el = scrollRef.current;
    if (el) stick.current = el.scrollHeight - el.scrollTop - el.clientHeight < 80;
  };

  // Follow new content by setting scrollTop directly (no scrollIntoView fighting
  // the browser's scroll anchoring), and inside rAF so layout has settled first —
  // together with plain-text streaming this keeps the view from jittering. The
  // dependency tracks rendered content, not just array identity, so streaming
  // still follows the bottom if a reducer reuses the items array.
  const contentVersion = scrollVersion(items);
  useEffect(() => {
    if (!stick.current) return;
    const el = scrollRef.current;
    if (!el) return;
    const id = requestAnimationFrame(() => {
      el.scrollTop = el.scrollHeight;
    });
    return () => cancelAnimationFrame(id);
  }, [contentVersion]);

  useEffect(() => {
    const el = scrollRef.current;
    if (!el || typeof ResizeObserver === "undefined") return;
    lastClientHeight.current = el.clientHeight;
    const observer = new ResizeObserver(() => {
      const previous = lastClientHeight.current ?? el.clientHeight;
      lastClientHeight.current = el.clientHeight;
      repinIfWasPinned(el, stick, resizeFrame, el.clientHeight - previous);
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

  useEffect(() => {
    const el = scrollRef.current;
    if (!el) return;
    const previous = lastFooterHeight.current ?? footerHeight;
    lastFooterHeight.current = footerHeight;
    repinIfWasPinned(el, stick, resizeFrame, previous - footerHeight);
    return () => {
      if (resizeFrame.current !== null) {
        cancelAnimationFrame(resizeFrame.current);
        resizeFrame.current = null;
      }
    };
  }, [footerHeight]);

  // Sub-agent calls carry a parentId; collect them under their parent `task`
  // call so the parent card can render them nested, and skip them at top level.
  const subcallsByParent = new Map<string, ToolItem[]>();
  for (const it of items) {
    if (it.kind === "tool" && it.parentId) {
      const arr = subcallsByParent.get(it.parentId) ?? [];
      arr.push(it);
      subcallsByParent.set(it.parentId, arr);
    }
  }

  // The rewind menu's open state is lifted here so at most one is open at a time;
  // a mousedown outside any .rewind closes it.
  const [openTurn, setOpenTurn] = useState<number | null>(null);
  useEffect(() => {
    if (openTurn === null) return;
    const onDown = (e: MouseEvent) => {
      const el = e.target as Element | null;
      if (!el || !el.closest(".rewind")) setOpenTurn(null);
    };
    document.addEventListener("mousedown", onDown);
    return () => document.removeEventListener("mousedown", onDown);
  }, [openTurn]);

  // Each user message's turn = its ordinal among user messages, so a rewind
  // targets the matching checkpoint.
  const userTurn = new Map<string, number>();
  let nt = 0;
  for (const it of items) {
    if (it.kind === "user") userTurn.set(it.id, nt++);
  }

  return (
    <div className="transcript" ref={scrollRef} onScroll={onScroll}>
      {items.length === 0 && <Welcome onPrompt={onPrompt} />}

      {items.map((it) => {
        switch (it.kind) {
          case "user": {
            const tn = userTurn.get(it.id);
            return (
              <UserMessage
                key={it.id}
                text={it.text}
                turn={tn}
                open={tn != null && openTurn === tn}
                onToggle={() => setOpenTurn((cur) => (cur === tn ? null : (tn ?? null)))}
                onRewind={(turn, scope) => {
                  onRewind?.(turn, scope);
                  setOpenTurn(null);
                }}
              />
            );
          }
          case "assistant":
            return <AssistantMessage key={it.id} item={it} />;
          case "tool":
            if (it.parentId) return null; // rendered nested under its parent
            if (it.name === "todo_write") return null; // shown live in the pinned TodoPanel
            if (it.name === "exit_plan_mode") return null; // the plan was shown in the approval card
            return <ToolCard key={it.id} item={it} subcalls={subcallsByParent.get(it.id)} />;
          case "phase":
            return (
              <div key={it.id} className="phase">
                {it.text}
              </div>
            );
          case "notice":
            return (
              <div key={it.id} className={`notice notice--${it.level}`}>
                {it.text}
              </div>
            );
          case "compaction":
            return <CompactionCard key={it.id} item={it} />;
        }
      })}
    </div>
  );
}

type CompactionItem = Extract<Item, { kind: "compaction" }>;

// CompactionCard marks a context-compaction boundary in the transcript. While
// the pass runs it shows a "compacting…" placeholder; once done it shows the
// message count and trigger with the summary collapsed behind a toggle (the
// summary is the new context base, so it's available but doesn't flood the view).
function CompactionCard({ item }: { item: CompactionItem }) {
  const [open, setOpen] = useState(false);
  if (item.pending) {
    return (
      <div className="compaction compaction--pending">
        <span className="compaction__spinner">⋯</span> Compacting conversation…
      </div>
    );
  }
  return (
    <div className="compaction">
      <button className="compaction__head" onClick={() => setOpen((v) => !v)}>
        <span className="compaction__icon">◆</span>
        <span className="compaction__title">Context compacted</span>
        <span className="compaction__meta">
          {item.messages} messages · {item.trigger}
        </span>
        <span className="compaction__toggle">{open ? "hide summary" : "show summary"}</span>
      </button>
      {open && <pre className="compaction__summary">{item.summary}</pre>}
    </div>
  );
}
