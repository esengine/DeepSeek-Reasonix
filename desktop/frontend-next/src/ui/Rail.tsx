import { useCallback, useEffect, useMemo, useRef, useState, type CSSProperties, type RefObject } from "react";
import { t } from "../i18n";

export interface RailMark {
  id: string;
  text: string;
  // Which block holds it and where inside that block, because the block is what
  // survives virtualisation: an unmounted one is a placeholder with the right
  // height, so its position is known even when the message itself has no node.
  // Position in the transcript, in entries. The rail no longer places by it
  // (see layout), but a jump still lands by block and offset.
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
   *  because the same position in two conversations is two different questions. */
  scope: string;
  // Only the transcript on screen answers the keys; a pane on another tab keeps
  // its rail but must not fight for them.
  bound: boolean;
}

// The native scrollbar owns continuous position. The semantic rail is a compact
// fisheye index, like Codex: questions stay together in reading order and only
// the neighbourhood under the pointer expands. Spreading sparse turns across
// the full height makes two messages look like unrelated controls.
const STEP = 9;
const PAD = 26;

// Marks closer than this are not told apart by eye or by pointer, so only one of
// them is drawn; the rest stay addressable through their positions.
const MIN_GAP = 2;

// Below this spacing a mark stops being a mark and becomes part of a crowd: the
// lines blur into one another and no pointer can pick out the fourth of nine.
const CLUSTER_AT = 5;

// Length and weight fall away from whichever mark the pointer has picked out,
// and every mark is the same until one is picked. A cosine rather than a line:
// a linear falloff has a corner at the focus, which reads as the neighbours
// being a different kind of thing rather than the same thing further away.
const REACH = 5;

// A star belongs to one question in one conversation. It is stored by the
// question's position in the transcript, not by the row's id: `nextId` is a
// per-load counter, so "i7" names a different message after a reload — and the
// same "i7" in every other conversation.
const BOOKMARK_KEY = "rx-rail-marks";

function bookmarkKey(scope: string) {
  return `${BOOKMARK_KEY}:${scope}`;
}

function loadBookmarks(scope: string): Set<string> {
  try {
    const raw = localStorage.getItem(bookmarkKey(scope));
    const list = raw ? (JSON.parse(raw) as unknown) : [];
    return new Set(Array.isArray(list) ? list.filter((x): x is string => typeof x === "string") : []);
  } catch {
    return new Set();
  }
}

function saveBookmarks(scope: string, marks: Set<string>) {
  try {
    localStorage.setItem(bookmarkKey(scope), JSON.stringify([...marks]));
  } catch {
    // A full or blocked store leaves the star in memory for this session; the
    // rail is not the place to report it.
  }
}

function layout(count: number, rail: number) {
  if (count <= 0) return [] as number[];
  const room = Math.max(0, rail - PAD * 2);
  const step = count > 1 ? Math.min(STEP, room / (count - 1)) : STEP;
  const span = (count - 1) * step;
  const start = rail / 2 - span / 2;
  return Array.from({ length: count }, (_, i) => start + i * step);
}

/** One drawn thing: a single mark, or a run of them folded together.
 *
 *  Folding only starts where reading them apart has already failed: while the
 *  spacing is at least CLUSTER_AT every question keeps its own line, so a
 *  conversation of ordinary length looks exactly as it did before. */
export interface RailView {
  top: number;
  from: number;
  to: number;
  single: boolean;
}

export function cluster(count: number, rail: number): RailView[] | null {
  if (count <= 0) return null;
  const room = Math.max(0, rail - PAD * 2);
  const step = count > 1 ? Math.min(STEP, room / (count - 1)) : STEP;
  if (step >= CLUSTER_AT) return null;
  const slots = Math.max(2, Math.floor(room / STEP) + 1);
  const per = Math.ceil(count / slots);
  const n = Math.ceil(count / per);
  const gap = n > 1 ? Math.min(STEP, room / (n - 1)) : STEP;
  const span = (n - 1) * gap;
  const start = rail / 2 - span / 2;
  return Array.from({ length: n }, (_, i) => ({
    top: start + i * gap,
    from: i * per,
    to: Math.min(count - 1, (i + 1) * per - 1),
    single: per === 1,
  }));
}

/** How strongly a mark answers the focus: 1 at it, 0 beyond REACH. */
function pull(i: number, focus: number): number {
  if (focus < 0) return 0;
  const d = Math.abs(i - focus);
  return d > REACH ? 0 : (Math.cos((Math.PI * d) / REACH) + 1) / 2;
}

/** Convert a pointer position from the screen-sized rectangle into the rail's
 * layout coordinate system. CSS zoom changes the former but clientHeight and
 * the mark tops stay in the latter, so subtracting the two directly drifts in
 * compact and enlarged interface modes. */
export function railLocalY(clientY: number, rectTop: number, rectHeight: number, railHeight: number): number {
  if (rectHeight <= 0 || railHeight <= 0) return 0;
  return (clientY - rectTop) * (railHeight / rectHeight);
}

export function Rail({ marks, total, scroll, flow, onJump, bound, scope }: Props) {
  const host = useRef<HTMLDivElement>(null);
  const [tops, setTops] = useState<number[]>([]);
  // Null while every question keeps its own line; a list once they have to
  // share, because reading them apart has stopped working.
  const [groups, setGroups] = useState<RailView[] | null>(null);
  const [at, setAt] = useState(-1);
  // Which mark the keys last walked to. Kept apart from `at`, which the pointer
  // owns: hovering somewhere else must not move where the keys resume from.
  const here = useRef(-1);
  // Which questions the reader starred. Kept by message id so folding the rail
  // around them cannot lose one.
  const [stars, setStars] = useState<Set<string>>(() => loadBookmarks(scope));
  useEffect(() => setStars(loadBookmarks(scope)), [scope]);
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
  // Rail height is read when the surface changes rather than on every streamed
  // frame.
  const geom = useRef({ rail: 1 });

  const measure = useCallback(() => {
    const root = scroll.current;
    const inner = flow.current;
    const box = host.current;
    if (!root || !inner || !box) return;
    const rail = root.clientHeight;
    box.style.setProperty("--rail-h", rail + "px");
    geom.current = { rail };
    setTops(layout(marks.length, rail));
    setGroups(cluster(marks.length, rail));
  }, [marks, total, scroll, flow]);

  // Deferred for the same reason the size observer below is: measure() reads
  // clientHeight and scrollHeight, and running that inside the commit that just
  // mounted a transcript forces a whole-document layout. One frame later the
  // browser has done it anyway.
  useEffect(() => {
    const raf = requestAnimationFrame(measure);
    return () => cancelAnimationFrame(raf);
  }, [measure]);

  useEffect(() => {
    const root = scroll.current;
    const inner = flow.current;
    if (!root || !inner) return;
    // One measurement per frame, not one per mutation. Mounting a long
    // transcript resizes the content once per block, and measure() reads
    // clientHeight and scrollHeight — a forced layout apiece, then a state
    // update that renders the rail again.
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
    return () => {
      if (raf) cancelAnimationFrame(raf);
      ro.disconnect();
    };
  }, [scroll, flow, measure]);

  // Ctrl/⌘ + ↑/↓ walks the marks. Which one you are "at" is decided by the viewport,
  // not by a cursor the rail would have to keep: the nearest mark above the top
  // of the view is where you are, so the keys agree with what you can see.
  useEffect(() => {
    if (!bound) return;
    const onKey = (e: KeyboardEvent) => {
      if (!(e.metaKey || e.ctrlKey) || (e.key !== "ArrowUp" && e.key !== "ArrowDown")) return;
      const el = e.target as HTMLElement | null;
      if (el && (el.tagName === "TEXTAREA" || el.tagName === "INPUT" || el.isContentEditable)) return;
      const root = scroll.current;
      if (!root || tops.length === 0) return;
      const up = e.key === "ArrowUp";
      // Walk the marks themselves. The nearest starting point is derived from
      // the same transcript fraction used by the overview ruler.
      let pick: number;
      if (here.current >= 0) {
        pick = here.current + (up ? -1 : 1);
      } else {
        // Nothing walked yet: start from whichever mark the viewport is nearest,
        // which is the one fraction of the transcript it is showing. Measured
        // against where each message sits in the transcript, not against the
        // overview track rather than any currently mounted card geometry.
        const seen = Math.min(1, root.scrollTop / Math.max(1, root.scrollHeight - root.clientHeight));
        const place = (i: number) => marks[i].at / Math.max(1, total);
        const near = marks.reduce((best, _m, i) => (Math.abs(place(i) - seen) < Math.abs(place(best) - seen) ? i : best), 0);
        pick = up ? near : Math.min(near + 1, tops.length - 1);
      }
      if (pick < 0 || pick >= marks.length) return;
      here.current = pick;
      e.preventDefault();
      onJump(marks[pick]);
      const slot = view.findIndex((v) => pick >= v.from && pick <= v.to);
      if (slot >= 0) {
        setAt(slot);
        setTimeout(() => setAt((v) => (v === slot ? -1 : v)), 1400);
      }
    };
    addEventListener("keydown", onKey);
    return () => removeEventListener("keydown", onKey);
  }, [bound, tops, marks, total, scroll, onJump]);

  const drawn = useMemo(() => {
    if (tops.length !== marks.length) return marks.map((_m, i) => i);
    const out: number[] = [];
    let last = -Infinity;
    tops.forEach((top, i) => {
      if (top - last >= MIN_GAP || i === tops.length - 1) {
        out.push(i);
        last = top;
      }
    });
    return out;
  }, [tops, marks]);

  // What is drawn: one line per question while the spacing holds, a folded run
  // per line once it does not. `at` indexes this list either way. `groups` is a
  // state slice, so after a rewind drops questions it can still name an index
  // the shorter `marks` no longer has; the fold is filtered to what exists.
  const view: RailView[] = useMemo(
    () => (groups ?? drawn.map((i) => ({ top: tops[i] ?? 0, from: i, to: i, single: true })))
      .filter((v) => v.from < marks.length),
    [groups, drawn, tops, marks.length],
  );

  // The pointer's y decides which mark it means. Hitting a three-pixel line is
  // not a thing anyone should have to do, and a mark that moved under the
  // pointer to acknowledge the hover would take itself out from under it.
  const aim = (y: number) => {
    let best = -1;
    let dist = Infinity;
    (groups ? groups.map((g) => g.top) : tops).forEach((top, i) => {
      const d = Math.abs(top - y);
      if (d < dist) {
        dist = d;
        best = i;
      }
    });
    // Give every two-pixel mark a generous hit corridor; the visible line can
    // stay quiet without demanding pixel-perfect pointing.
    setAt(dist <= 40 ? best : -1);
  };

  if (marks.length === 0) return null;
  const shown = at >= 0 && at < view.length ? view[at] : null;
  const head = shown ? marks[shown.from] : null;

  return (
    <div className="railhost" ref={host} aria-hidden={undefined}>
      <nav
        className="srail"
        aria-label={t("你说过的话")}
        onMouseMove={(e) => {
          const rect = e.currentTarget.getBoundingClientRect();
          aim(railLocalY(e.clientY, rect.top, rect.height, geom.current.rail));
        }}
        onMouseLeave={() => setAt(-1)}
        onClick={(e) => {
          if (e.target !== e.currentTarget) return;
          if (shown) onJump(marks[shown.from]);
        }}
      >
        {view.map((v, i) => {
          const m = marks[v.from];
          const lit = pull(i, at);
          const folded = !v.single;
          // A folded run counts as starred when any question inside it is.
          let starred = m.msgIndex !== undefined && stars.has(String(m.msgIndex));
          if (!starred && folded) {
            for (let k = v.from; k <= v.to; k++) {
              const inner = marks[k].msgIndex;
              if (inner !== undefined && stars.has(String(inner))) { starred = true; break; }
            }
          }
          return (
            <button
              key={folded ? `fold-${v.from}` : m.id}
              className="srail-m"
              style={{ top: v.top }}
              data-on={i === at ? "" : undefined}
              data-folded={folded ? "" : undefined}
              data-starred={starred ? "" : undefined}
              aria-label={folded
                ? t("定位到第 {a} 至 {b} 条", { a: v.from + 1, b: v.to + 1 })
                : t("定位到你说的：{text}", { text: m.text.slice(0, 40) })}
              data-action-contextmenu="rail.star"
              data-action-keydown="rail.star"
              title={m.msgIndex === undefined ? undefined : starred ? t("右键取消这处星标") : t("右键给这处加星标")}
              onFocus={() => setAt(i)}
              onBlur={() => setAt(-1)}
              onClick={() => onJump(m)}
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
              {/* 会动的是这条线，按钮本身不动 —— 它一动就会从指针底下跑掉。
                  长度和浓淡都是到焦点的距离的函数，静止时人人相等：尺寸原来
                  跟着这一轮碰过的文件数走，那让一列本该等价的入口长短不一，
                  而「碰了几个文件」这句话悬停时就在旁边说得清楚。 */}
              <i style={{ "--lit": lit.toFixed(3) } as CSSProperties} />
            </button>
          );
        })}
      </nav>
      {/* 贴着轨道两端的那几条，预览要收进容器里 —— 否则它会被裁掉一半 */}
      {shown && (
        <div
          className="srail-peek"
          style={{ top: Math.min(Math.max(shown.top, 44), Math.max(44, geom.current.rail - 44)) }}
          role="tooltip"
        >
          <div className="hd">
            <span className="n">
              {shown.single
                ? t("你的第 {n} 条消息", { n: shown.from + 1 })
                : t("第 {a} 至 {b} 条", { a: shown.from + 1, b: shown.to + 1 })}
            </span>
            {head && head.files > 0 && <span>{t("{n} 个文件", { n: head.files })}</span>}
          </div>
          <div className="tx">{head?.text}</div>
        </div>
      )}
    </div>
  );
}
