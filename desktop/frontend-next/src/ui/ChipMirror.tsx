import { useCallback, useLayoutEffect, useRef, useState, type RefObject } from "react";
import type { ChipCall, Completion, CompletionItem } from "../port/port";
import { chipText, place, reconcile, snap, split, touches, type SkillChip } from "./skillchips";

interface Held {
  // The text these chips are positioned in. Text that moves away from it is an
  // edit the chips have not been carried across yet.
  base: string;
  list: SkillChip[];
}

// useSkillChips keeps the chips in step with a plain textarea. Every change to
// the text, typed or set by code, is reconciled from the text the chips were
// last placed in, so no path into the box has to remember to tell them.
export function useSkillChips(box: RefObject<HTMLTextAreaElement | null>, text: string, set: (text: string, caret: number) => void) {
  const [held, setHeld] = useState<Held>({ base: "", list: [] });
  useLayoutEffect(() => {
    if (held.base === text) return;
    const el = box.current;
    const r = reconcile(held.base, text, held.list, el ? el.selectionStart : undefined);
    setHeld({ base: r.text, list: r.chips });
    if (r.text !== text) set(r.text, r.caret ?? r.text.length);
  }, [text, held, box, set]);
  const list = held.base === text ? held.list : [];
  return {
    list,
    held: () => held,
    restore: (h: Held) => setHeld(h),
    place: (at: string, c: Completion, item: CompletionItem) => {
      const r = c.kind === "slash" ? place(at, c.from, c.to, list, item) : null;
      if (r) setHeld({ base: r.text, list: r.chips });
      return r;
    },
    touches: (caret: number) => touches(list, caret),
    snap: (el: HTMLTextAreaElement, before: number) => {
      if (el.selectionStart !== el.selectionEnd) return el.selectionStart;
      const at = snap(list, el.selectionStart, before);
      if (at !== el.selectionStart) el.setSelectionRange(at, at);
      return at;
    },
    call: (compose: (v: string) => string): ChipCall | undefined => {
      if (list.length === 0) return undefined;
      const s = split(text, list);
      return { submit: compose(s.submit), invocations: s.invocations };
    },
  };
}

const COPIED = [
  "font-family", "font-size", "font-weight", "font-style", "font-feature-settings", "font-variation-settings",
  "letter-spacing", "word-spacing", "line-height", "text-indent", "text-transform", "tab-size",
  "padding-top", "padding-right", "padding-bottom", "padding-left", "direction",
];

// The chips drawn over the textarea's own glyphs, at the same metrics, so the
// caret and selection stay the textarea's while the words take the chip's look.
// Scrolling follows the textarea's scroll timeline in CSS; --travel is the
// distance that timeline spans.
export function ChipMirror({ box, text, chips }: { box: RefObject<HTMLTextAreaElement | null>; text: string; chips: SkillChip[] }) {
  const mirror = useRef<HTMLDivElement>(null);
  const sync = useCallback(() => {
    const el = box.current;
    const m = mirror.current;
    if (!el || !m) return;
    const cs = getComputedStyle(el);
    for (const k of COPIED) m.style.setProperty(k, cs.getPropertyValue(k));
    m.style.left = `${el.offsetLeft}px`;
    m.style.top = `${el.offsetTop}px`;
    m.style.width = `${el.clientWidth}px`;
    m.style.height = `${el.clientHeight}px`;
    m.style.setProperty("--travel", `${el.scrollHeight - el.clientHeight}px`);
  }, [box]);
  useLayoutEffect(() => {
    sync();
    const el = box.current;
    if (!el || chips.length === 0 || typeof ResizeObserver === "undefined") return;
    const ro = new ResizeObserver(sync);
    ro.observe(el);
    return () => ro.disconnect();
  });
  if (chips.length === 0) return null;
  const parts: React.ReactNode[] = [];
  let from = 0;
  for (const c of chips) {
    parts.push(text.slice(from, c.at));
    parts.push(<span key={c.at} className="skillchip" data-kind={c.kind}>{chipText(c)}</span>);
    from = c.at + chipText(c).length;
  }
  parts.push(text.slice(from) + "\u200b");
  return (
    <div ref={mirror} className="chipmirror" aria-hidden="true">
      <div className="chipflow">{parts}</div>
    </div>
  );
}
