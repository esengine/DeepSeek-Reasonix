import { useCallback, useEffect, useMemo, useRef, useState, type CSSProperties, type RefObject } from "react";
import { t } from "../i18n";
import { renderedBox } from "./findland";
import { StudioIcon } from "./StudioIcon";
import { fold, grow, HEAD, nearest, place, viewBand, within, type RailView } from "./railgeom";

export interface RailMark {
  id: string;
  text: string;
  // Position in the transcript, in entries: where the mark is drawn while the
  // blocks have no measured geometry to place it by.
  at: number;
  // The message's index in the session log, which survives a reload: `at` is an
  // index into the rendered list, and that list drops host chrome, notices and
  // tool rows, so it shifts. Absent for a mark whose item has no logged index.
  msgIndex?: number;
  // Which block holds it, for a jump: the block is what exists while the
  // message itself is unmounted.
  block: number;
  within: number;
  of: number;
  files: number;
}

interface Props {
  marks: RailMark[];
  // How many entries the transcript holds, which is what a mark's position is a
  // fraction of.
  total: number;
  scroll: RefObject<HTMLDivElement | null>;
  flow: RefObject<HTMLDivElement | null>;
  onJump: (mark: RailMark) => void;
  /** Which conversation these marks belong to. Stars are stored per conversation,
   *  because the same position in two conversations is two different questions.
   *  Empty until the conversation is saved: its stars are then kept in memory. */
  scope: string;
  // Only the transcript on screen answers the keys; a pane on another tab keeps
  // its rail but must not fight for them.
  bound: boolean;
  /** Raw messages still unloaded before the first mark. A paged conversation
   *  mounts only its tail, so the rail would otherwise look complete while most
   *  of the conversation is still on disk. */
  earlier?: number;
  onLoadEarlier?: () => void;
}

// A star belongs to one question in one conversation. It is stored by the
// question's position in the transcript, not by the row's id: `nextId` is a
// per-load counter, so "i7" names a different message after a reload — and the
// same "i7" in every other conversation.
const BOOKMARK_KEY = "rx-rail-marks";

function bookmarkKey(scope: string) {
  return `${BOOKMARK_KEY}:${scope}`;
}

function loadBookmarks(scope: string): Set<string> {
  if (!scope) return new Set();
  try {
    const raw = localStorage.getItem(bookmarkKey(scope));
    const list = raw ? (JSON.parse(raw) as unknown) : [];
    return new Set(Array.isArray(list) ? list.filter((x): x is string => typeof x === "string") : []);
  } catch {
    return new Set();
  }
}

function saveBookmarks(scope: string, marks: Set<string>) {
  if (!scope) return;
  try {
    localStorage.setItem(bookmarkKey(scope), JSON.stringify([...marks]));
  } catch {
    // A full or blocked store leaves the star in memory for this session; the
    // rail is not the place to report it.
  }
}

/** Convert a pointer position from the screen-sized rectangle into the rail's
 * layout coordinate system. CSS zoom changes the former but clientHeight and
 * the mark tops stay in the latter, so subtracting the two directly drifts in
 * compact and enlarged interface modes. */
export function railLocalY(clientY: number, rectTop: number, rectHeight: number, railHeight: number): number {
  if (rectHeight <= 0 || railHeight <= 0) return 0;
  return (clientY - rectTop) * (railHeight / rectHeight);
}


// Where each mark sits in the scrolled content, as a fraction of its height: a
// mounted message by its own box, an unmounted one by its offset inside its
// block, which is mounted or a placeholder holding the block's real height.
function fractions(marks: RailMark[], total: number, root: HTMLElement, inner: HTMLElement): number[] {
  const height = root.scrollHeight;
  const chunks = inner.querySelectorAll<HTMLElement>(".chunk");
  const base = root.getBoundingClientRect().top - root.scrollTop;
  const boxes = [...chunks].map((c) => c.getBoundingClientRect());
  const measured = height > 0 && marks.every((m) => (boxes[m.block]?.height ?? 0) > 0);
  if (!measured) return marks.map((m) => m.at / Math.max(1, total - 1));
  return marks.map((m) => {
    const own = inner.querySelector(`[data-item="${CSS.escape(m.id)}"]`);
    const box = own ? renderedBox(own as HTMLElement).getBoundingClientRect() : null;
    if (box && box.height > 0) return (box.top - base) / height;
    const block = boxes[m.block];
    return (block.top - base + (m.of > 1 ? m.within / m.of : 0) * block.height) / height;
  });
}

const StarGlyph = () => (
  <svg className="srail-star" viewBox="0 0 16 16" aria-hidden="true"><path d="M8 2.2l1.7 3.6 3.9.5-2.9 2.7.8 3.9L8 11l-3.5 1.9.8-3.9-2.9-2.7 3.9-.5z" /></svg>
);

export function Rail({ marks, total, scroll, flow, onJump, bound, scope, earlier = 0, onLoadEarlier }: Props) {
  const host = useRef<HTMLDivElement>(null);
  const [tops, setTops] = useState<number[]>([]);
  const [band, setBand] = useState<{ top: number; height: number } | null>(null);
  // The drawn thing the pointer or the keys picked out; `keyed` when the keys did.
  const [at, setAt] = useState(-1);
  const [keyed, setKeyed] = useState(false);
  // Which run has been opened into its own messages. A run is one segment
  // standing for many; opening it lets the reader pick the one they meant.
  const [opened, setOpened] = useState<number | null>(null);
  // The pointer crosses a gap between a run and the list beside it, so closing
  // waits briefly instead of dropping the list the moment the line is left.
  const closer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  const hold = useCallback(() => {
    if (closer.current !== undefined) { clearTimeout(closer.current); closer.current = undefined; }
  }, []);
  const release = useCallback(() => {
    hold();
    closer.current = setTimeout(() => setOpened(null), 200);
  }, [hold]);
  useEffect(() => () => { hold(); }, [hold]);
  // Which mark the keys last walked to. Kept apart from `at`, which the pointer
  // owns: hovering somewhere else must not move where the keys resume from.
  const here = useRef(-1);
  const [stars, setStars] = useState<Set<string>>(() => loadBookmarks(scope));
  const starScope = useRef(scope);
  useEffect(() => {
    const from = starScope.current;
    starScope.current = scope;
    if (from === scope) return;
    setStars((prev) => {
      // The first save names the conversation the stars were placed in.
      if (from || !scope || prev.size === 0) return loadBookmarks(scope);
      const next = new Set([...loadBookmarks(scope), ...prev]);
      saveBookmarks(scope, next);
      return next;
    });
  }, [scope]);
  const star = useCallback((msgIndex: number | undefined) => {
    if (msgIndex === undefined) return;
    const key = String(msgIndex);
    setStars((prev) => {
      const next = new Set(prev);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      saveBookmarks(scope, next);
      return next;
    });
  }, [scope]);
  const starred = (m: RailMark | undefined) => m?.msgIndex !== undefined && stars.has(String(m.msgIndex));
  const head = earlier > 0 && onLoadEarlier ? HEAD : 0;
  const geom = useRef({ rail: 1 });

  const follow = useCallback(() => {
    const root = scroll.current;
    if (root) setBand(viewBand(root.scrollTop, root.clientHeight, root.scrollHeight, geom.current.rail, head));
  }, [scroll, head]);

  const measure = useCallback(() => {
    const root = scroll.current;
    const inner = flow.current;
    const box = host.current;
    const rail = root?.clientHeight ?? 0;
    // A rail with no height is one not laid out yet, not one every mark shares.
    if (!root || !inner || !box || rail <= 0) return;
    box.style.setProperty("--rail-h", rail + "px");
    geom.current = { rail };
    setTops(place(fractions(marks, total, root, inner), rail, head));
    follow();
  }, [marks, total, scroll, flow, head, follow]);

  // A frame late: measure() reads layout, and inside the commit that mounted a
  // transcript that read would force a whole-document layout of its own.
  useEffect(() => {
    const raf = requestAnimationFrame(measure);
    return () => cancelAnimationFrame(raf);
  }, [measure]);

  useEffect(() => {
    const root = scroll.current;
    const inner = flow.current;
    if (!root || !inner) return;
    // One measurement per frame, not one per block that resized while mounting.
    let raf = 0;
    const ro = new ResizeObserver(() => {
      if (raf) return;
      raf = requestAnimationFrame(() => {
        raf = 0;
        measure();
      });
    });
    ro.observe(inner);
    ro.observe(root);
    let moved = 0;
    const onScroll = () => {
      if (!moved) moved = requestAnimationFrame(() => { moved = 0; follow(); });
    };
    root.addEventListener("scroll", onScroll, { passive: true });
    return () => {
      if (raf) cancelAnimationFrame(raf);
      if (moved) cancelAnimationFrame(moved);
      ro.disconnect();
      root.removeEventListener("scroll", onScroll);
    };
  }, [scroll, flow, measure, follow]);

  // Until the first measurement every mark is drawn on its own, unplaced, so
  // the marks exist to be named and focused from the first frame.
  const view: RailView[] = useMemo(
    () => (tops.length === marks.length ? fold(tops) : marks.map((_m, i) => ({ top: 0, bottom: 0, from: i, to: i, single: true }))),
    [tops, marks],
  );

  // Ctrl/⌘ + ↑/↓ walks the marks. Until the keys have walked anywhere, where
  // you are is the viewport: the mark nearest the top of what you can see.
  useEffect(() => {
    if (!bound) return;
    let fade: ReturnType<typeof setTimeout> | undefined;
    const onKey = (e: KeyboardEvent) => {
      if (!(e.metaKey || e.ctrlKey) || (e.key !== "ArrowUp" && e.key !== "ArrowDown")) return;
      const el = e.target as HTMLElement | null;
      if (el && (el.tagName === "TEXTAREA" || el.tagName === "INPUT" || el.isContentEditable)) return;
      const root = scroll.current;
      if (!root || tops.length === 0) return;
      const up = e.key === "ArrowUp";
      let pick: number;
      if (here.current >= 0) {
        pick = here.current + (up ? -1 : 1);
      } else {
        const [seen] = place([root.scrollTop / Math.max(1, root.scrollHeight)], geom.current.rail, head);
        const near = tops.reduce((best, top, i) => (Math.abs(top - seen) < Math.abs(tops[best] - seen) ? i : best), 0);
        pick = up ? near : Math.min(near + 1, tops.length - 1);
      }
      if (pick < 0 || pick >= marks.length) return;
      here.current = pick;
      e.preventDefault();
      onJump(marks[pick]);
      const slot = view.findIndex((v) => pick >= v.from && pick <= v.to);
      if (slot < 0) return;
      setAt(slot);
      setKeyed(true);
      clearTimeout(fade);
      fade = setTimeout(() => { setAt((v) => (v === slot ? -1 : v)); setKeyed(false); }, 1400);
    };
    addEventListener("keydown", onKey);
    return () => { clearTimeout(fade); removeEventListener("keydown", onKey); };
  }, [bound, tops, marks, view, head, scroll, onJump]);

  // The pointer's y decides which mark it means, so nobody has to hit a 2px
  // line, and a line growing under the pointer cannot take itself out from under it.
  const aim = (y: number) => {
    const next = nearest(view, y);
    setAt(next);
    setKeyed(false);
    const hit = next >= 0 ? view[next] : null;
    if (!hit) return;
    hold();
    setOpened(hit.single ? null : next);
  };

  if (marks.length < 3) return null;
  const shown = opened === null && at >= 0 && at < view.length && view[at].single ? view[at] : null;
  const peek = shown ? marks[shown.from] : null;
  const run = opened !== null ? view[opened] : undefined;
  const clamp = (y: number, edge: number) => Math.min(Math.max(y, edge), Math.max(edge, geom.current.rail - edge));

  return (
    <div className="railhost" ref={host}>
      {/* A paged conversation mounts only its tail; without this the rail
          would read as the whole of a conversation that is mostly on disk. */}
      {head > 0 && (
        <button type="button" className="srail-earlier" data-action="history.more" onClick={onLoadEarlier}
          aria-label={t("chat.n_more_messages_before_these", { n: earlier })}>
          <StudioIcon name="arrow" />
          {earlier}
          <span className="srail-tip" aria-hidden="true">{t("chat.n_more_messages_before_these", { n: earlier })}</span>
        </button>
      )}
      <nav
        className="srail"
        aria-label={t("chat.what_you_said")}
        data-action-click="rail.jump"
        onMouseMove={(e) => {
          const rect = e.currentTarget.getBoundingClientRect();
          aim(railLocalY(e.clientY, rect.top, rect.height, geom.current.rail));
        }}
        onMouseLeave={() => { setAt(-1); release(); }}
        onClick={(e) => {
          if (e.target !== e.currentTarget || at < 0 || at >= view.length) return;
          const rect = e.currentTarget.getBoundingClientRect();
          onJump(marks[within(tops, view[at], railLocalY(e.clientY, rect.top, rect.height, geom.current.rail))]);
        }}
      >
        {band && <div className="srail-band" style={{ top: band.top, height: band.height }} />}
        {view.map((v, i) => {
          const m = marks[v.from];
          const folded = !v.single;
          let lit = starred(m);
          for (let k = v.from + 1; !lit && k <= v.to; k++) lit = starred(marks[k]);
          const seen = band !== null && v.bottom >= band.top && v.top <= band.top + band.height;
          const base = folded ? 12 : m.files > 0 ? 14 : 10;
          const tall = Math.max(4, v.bottom - v.top);
          return (
            <button
              key={folded ? `fold-${v.from}` : m.id}
              type="button"
              className="srail-m"
              style={{ top: (v.top + v.bottom) / 2, ...(folded ? { height: tall + 8, marginTop: -(tall + 8) / 2 } : {}) }}
              data-on={i === at ? "" : undefined}
              data-keyed={i === at && keyed ? "" : undefined}
              data-folded={folded ? "" : undefined}
              data-starred={lit ? "" : undefined}
              data-seen={seen ? "" : undefined}
              data-edits={!folded && m.files > 0 ? "" : undefined}
              aria-label={folded
                ? t("chat.jump_to_questions_a_to", { a: v.from + 1, b: v.to + 1 })
                : t("chat.jump_to_what_you_said", { text: m.text.slice(0, 40) })}
              data-action-click="rail.jump"
              data-action-contextmenu="rail.star"
              data-action-keydown="rail.star"
              onFocus={(e) => { if (e.currentTarget.matches(":focus-visible")) setAt(i); }}
              onBlur={() => setAt(-1)}
              onClick={() => {
                if (!folded) { onJump(m); return; }
                if (opened === i) { setOpened(null); onJump(m); return; }
                setOpened(i);
              }}
              onContextMenu={(e) => {
                e.preventDefault();
                star(m.msgIndex);
              }}
              onKeyDown={(e) => {
                if (e.key !== "s" && e.key !== "S") return;
                e.preventDefault();
                star(m.msgIndex);
              }}
            >
              <i style={{ "--w": `${base + (at >= 0 && !folded ? grow(Math.abs(i - at)) : 0)}px`, ...(folded ? { "--h": `${tall}px` } : {}) } as CSSProperties} />
            </button>
          );
        })}
      </nav>
      {run && !run.single && (
        <div className="srail-sub" style={{ top: clamp((run.top + run.bottom) / 2, 150) }} onMouseEnter={hold} onMouseLeave={release}>
          <div className="srail-subhd">{t("chat.questions_a_to_b", { a: run.from + 1, b: run.to + 1 })}</div>
          {marks.slice(run.from, run.to + 1).map((mk, k) => (
            <button key={mk.id} type="button" className="srail-subrow" data-action="rail.pick" onClick={() => { setOpened(null); onJump(mk); }}>
              <span className="n">{run.from + k + 1}</span>
              <span className="tx">{mk.text.slice(0, 60)}</span>
              {starred(mk) && <StarGlyph />}
            </button>
          ))}
        </div>
      )}
      {shown && peek && (
        <div className="srail-peek" style={{ top: clamp(shown.top, 44) }} role="tooltip">
          <div className="hd">
            <span>{t("chat.your_message_n", { n: shown.from + 1 })}</span>
            {peek.files > 0 && <><span className="dot">·</span><span>{t("chat.changed_n_files", { n: peek.files })}</span></>}
          </div>
          <div className="tx">{peek.text}</div>
          <div className="keys">{t("chat.click_to_jump_right_click")}</div>
        </div>
      )}
    </div>
  );
}
