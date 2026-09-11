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
  // Dragging the box is the reader moving, and the follow has to be told so —
  // the same release a wheel gets.
  onGrab: () => void;
  // Only the transcript on screen answers the keys; a pane on another tab keeps
  // its rail but must not fight for them.
  bound: boolean;
}

// The rail is a fisheye, not a map. Marks sit together in the middle at a fixed
// step, so the list reads as a list — placing them by their position in the
// transcript drew how much each turn produced instead, and a turn that ran
// twenty tools landed ten times further from its neighbour than one that ran
// two (measured: gaps from 14 to 171px). Where the reader is in the transcript
// is what the viewport box already says; this is what you asked, in order.
const STEP = 9;
const PAD = 26;

// Length and weight fall away from whichever mark the pointer has picked out,
// and every mark is the same until one is picked. A cosine rather than a line:
// a linear falloff has a corner at the focus, which reads as the neighbours
// being a different kind of thing rather than the same thing further away.
const REACH = 5;

function layout(count: number, rail: number) {
  if (count <= 0) return { tops: [] as number[], step: STEP };
  const room = Math.max(0, rail - PAD * 2);
  const step = count > 1 ? Math.min(STEP, room / (count - 1)) : STEP;
  const span = (count - 1) * step;
  const start = rail / 2 - span / 2;
  return { tops: Array.from({ length: count }, (_, i) => start + i * step), step };
}

/** How strongly a mark answers the focus: 1 at it, 0 beyond REACH. */
function pull(i: number, focus: number): number {
  if (focus < 0) return 0;
  const d = Math.abs(i - focus);
  return d > REACH ? 0 : (Math.cos((Math.PI * d) / REACH) + 1) / 2;
}

export function Rail({ marks, total, scroll, flow, onJump, onGrab, bound }: Props) {
  const host = useRef<HTMLDivElement>(null);
  const [tops, setTops] = useState<number[]>([]);
  const [view, setView] = useState({ top: 0, height: 0 });
  const [at, setAt] = useState(-1);
  // Which mark the keys last walked to. Kept apart from `at`, which the pointer
  // owns: hovering somewhere else must not move where the keys resume from.
  const here = useRef(-1);
  // Content height and rail height, read when they change rather than per
  // frame: a follow writes scrollTop every frame and must not pay a layout for
  // this on the way.
  const geom = useRef({ content: 1, rail: 1 });

  const measure = useCallback(() => {
    const root = scroll.current;
    const inner = flow.current;
    const box = host.current;
    if (!root || !inner || !box) return;
    const rail = root.clientHeight;
    box.style.setProperty("--rail-h", rail + "px");
    geom.current = { content: root.scrollHeight || 1, rail };
    setTops(layout(marks.length, rail).tops);
    setView({
      top: (root.scrollTop / (root.scrollHeight || 1)) * rail,
      height: Math.max(16, (root.clientHeight / (root.scrollHeight || 1)) * rail),
    });
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
    // Scrolling only moves the viewport box, and that needs no measurement —
    // the two heights it divides by were captured when they last changed.
    const onScroll = () => {
      const { content, rail } = geom.current;
      setView((v) => ({ ...v, top: (root.scrollTop / content) * rail }));
    };
    root.addEventListener("scroll", onScroll, { passive: true });
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
      root.removeEventListener("scroll", onScroll);
      if (raf) cancelAnimationFrame(raf);
      ro.disconnect();
    };
  }, [scroll, flow, measure]);

  // ⌘↑ / ⌘↓ walk the marks. Which one you are "at" is decided by the viewport,
  // not by a cursor the rail would have to keep: the nearest mark above the top
  // of the view is where you are, so the keys agree with what you can see.
  useEffect(() => {
    if (!bound) return;
    const onKey = (e: KeyboardEvent) => {
      if (!e.metaKey || (e.key !== "ArrowUp" && e.key !== "ArrowDown")) return;
      const el = e.target as HTMLElement | null;
      if (el && (el.tagName === "TEXTAREA" || el.tagName === "INPUT" || el.isContentEditable)) return;
      const root = scroll.current;
      if (!root || tops.length === 0) return;
      const up = e.key === "ArrowUp";
      // Walk the marks themselves. Comparing the viewport's pixel position
      // against mark positions would mix two rulers — the marks are placed by
      // entry count now, precisely so that mounting cannot move them.
      let pick: number;
      if (here.current >= 0) {
        pick = here.current + (up ? -1 : 1);
      } else {
        // Nothing walked yet: start from whichever mark the viewport is nearest,
        // which is the one fraction of the transcript it is showing. Measured
        // against where each message sits in the transcript, not against the
        // rail — the rail is a cluster in the middle and its geometry says
        // nothing about how far down the record a mark is.
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
    // Anywhere in the cluster picks the nearest, plus a margin outside it so
    // the ends are no harder to reach than the middle. It was 120px, from when
    // the marks were spread over the whole track and the nearest one could be
    // that far; they now sit within a step of each other.
    setAt(dist <= 40 ? best : -1);
  };

  // Dragging the viewport box scrolls, because it is now the only scroll
  // indicator this transcript has — the native bar is hidden, two things saying
  // "you are here" side by side being the thing that made it look doubled.
  const drag = (e: React.PointerEvent) => {
    const root = scroll.current;
    const nav = e.currentTarget.parentElement;
    if (!root || !nav) return;
    e.preventDefault();
    e.stopPropagation();
    onGrab();
    const from = e.clientY;
    const start = root.scrollTop;
    const { rail } = geom.current;
    // What can be dragged is the track minus the box; what it covers is the
    // content minus one screen. Using rail-over-content instead makes every
    // drag fall behind the pointer by exactly the ratio of those two.
    //
    // Both are read per move, not captured on grab: blocks mount as the drag
    // travels and the content grows under it, which otherwise leaves the box
    // a few percent ahead of the pointer by the end. A drag is a handful of
    // events, so the layout it costs is nothing next to a streaming frame.
    const move = (ev: PointerEvent) => {
      const content = root.scrollHeight || 1;
      const box = Math.max(16, (root.clientHeight / content) * rail);
      const track = Math.max(1, rail - box);
      root.scrollTop = start + ((ev.clientY - from) / track) * Math.max(0, content - root.clientHeight);
    };
    const up = () => {
      removeEventListener("pointermove", move);
      removeEventListener("pointerup", up);
    };
    addEventListener("pointermove", move);
    addEventListener("pointerup", up);
  };

  if (marks.length === 0) return null;
  const shown = at >= 0 ? marks[at] : null;
  const viewPercent = Math.round((view.top / Math.max(1, geom.current.rail - view.height)) * 100);

  return (
    <div className="railhost" ref={host} aria-hidden={undefined}>
      <nav
        className="srail"
        aria-label={t("你说过的话")}
        onMouseMove={(e) => aim(e.clientY - e.currentTarget.getBoundingClientRect().top)}
        onMouseLeave={() => setAt(-1)}
        onClick={(e) => {
          if (e.target !== e.currentTarget) return;
          const i = at;
          if (i >= 0) onJump(marks[i]);
        }}
      >
        <button
          type="button"
          className="srail-view"
          data-action-pointerdown="transcript.scroll"
          data-action-keydown="transcript.scroll"
          style={{ top: view.top, height: view.height }}
          role="scrollbar"
          aria-label={t("你说过的话")}
          aria-controls="flowScroll"
          aria-orientation="vertical"
          aria-valuemin={0}
          aria-valuemax={100}
          aria-valuenow={Math.min(100, Math.max(0, viewPercent))}
          onPointerDown={drag}
          onKeyDown={(e) => {
            const root = scroll.current;
            if (!root) return;
            const page = Math.max(40, root.clientHeight * .82);
            if (e.key === "ArrowUp") root.scrollTop -= 40;
            else if (e.key === "ArrowDown") root.scrollTop += 40;
            else if (e.key === "PageUp") root.scrollTop -= page;
            else if (e.key === "PageDown") root.scrollTop += page;
            else if (e.key === "Home") root.scrollTop = 0;
            else if (e.key === "End") root.scrollTop = root.scrollHeight;
            else return;
            e.preventDefault();
            onGrab();
          }}
        />
        {marks.map((m, i) => {
          const lit = pull(i, at);
          return (
            <button
              key={m.id}
              className="srail-m"
              style={{ top: tops[i] ?? 0 }}
              data-on={i === at ? "" : undefined}
              aria-label={m.text.slice(0, 40)}
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
            <span className="n">{t("第 {n} 句", { n: at + 1 })}</span>
            {shown.files > 0 && <span>{t("{n} 个文件", { n: shown.files })}</span>}
          </div>
          <div className="tx">{shown.text}</div>
        </div>
      )}
    </div>
  );
}
