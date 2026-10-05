import { useCallback, useEffect, useRef, useState, type CSSProperties, type RefObject } from "react";
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

// Length and weight fall away from whichever mark the pointer has picked out,
// and every mark is the same until one is picked. A cosine rather than a line:
// a linear falloff has a corner at the focus, which reads as the neighbours
// being a different kind of thing rather than the same thing further away.
const REACH = 5;

function layout(count: number, rail: number) {
  if (count <= 0) return [] as number[];
  const room = Math.max(0, rail - PAD * 2);
  const step = count > 1 ? Math.min(STEP, room / (count - 1)) : STEP;
  const span = (count - 1) * step;
  const start = rail / 2 - span / 2;
  return Array.from({ length: count }, (_, i) => start + i * step);
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

export function Rail({ marks, total, scroll, flow, onJump, bound }: Props) {
  const host = useRef<HTMLDivElement>(null);
  const [tops, setTops] = useState<number[]>([]);
  const [at, setAt] = useState(-1);
  // Which mark the keys last walked to. Kept apart from `at`, which the pointer
  // owns: hovering somewhere else must not move where the keys resume from.
  const here = useRef(-1);
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
      setAt(pick);
      setTimeout(() => setAt((v) => (v === pick ? -1 : v)), 1400);
    };
    addEventListener("keydown", onKey);
    return () => removeEventListener("keydown", onKey);
  }, [bound, tops, marks, total, scroll, onJump]);

  // The pointer's y decides which mark it means. Hitting a three-pixel line is
  // not a thing anyone should have to do, and a mark that moved under the
  // pointer to acknowledge the hover would take itself out from under it.
  const aim = (y: number) => {
    let best = -1;
    let dist = Infinity;
    tops.forEach((top, i) => {
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
  const shown = at >= 0 ? marks[at] : null;

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
          const i = at;
          if (i >= 0) onJump(marks[i]);
        }}
      >
        {marks.map((m, i) => {
          const lit = pull(i, at);
          return (
            <button
              key={m.id}
              className="srail-m"
              style={{ top: tops[i] ?? 0 }}
              data-on={i === at ? "" : undefined}
              aria-label={t("定位到你说的：{text}", { text: m.text.slice(0, 40) })}
              onFocus={() => setAt(i)}
              onBlur={() => setAt(-1)}
              onClick={() => onJump(m)}
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
          style={{ top: Math.min(Math.max(tops[at] ?? 0, 44), Math.max(44, geom.current.rail - 44)) }}
          role="tooltip"
        >
          <div className="hd">
            <span className="n">{t("你的第 {n} 条消息", { n: at + 1 })}</span>
            {shown.files > 0 && <span>{t("{n} 个文件", { n: shown.files })}</span>}
          </div>
          <div className="tx">{shown.text}</div>
        </div>
      )}
    </div>
  );
}
