import { useRef } from "react";
import type { PointerEvent as ReactPointerEvent, KeyboardEvent as ReactKeyboardEvent } from "react";
import { t } from "../i18n";

export interface Span {
  min: number;
  max: number;
  def: number;
  key: string;
  // The remembered open width, and the width the column is actually drawn at.
  // They part company while a drag is shutting the panel: the column goes to
  // zero, the remembered width must not.
  css: string;
  col: string;
}

// 拖出来的宽度是读者对自己这块屏幕的判断，不是布局的默认值 —— 所以它归本地
// 存，不进内核配置：换台机器、换块屏幕，判断本来就不同。
export const RAIL: Span = { min: 232, max: 440, def: 264, key: "rx-rail-w", css: "--rail-open", col: "--rail-w" };
export const SIDE: Span = { min: 244, max: 540, def: 316, key: "rx-side-w", css: "--side-open", col: "--side-w" };
// The workbench beside the conversation. Wider than the rail because what sits
// in it is a page someone is reading, not a list of names.
export const DOCK: Span = { min: 320, max: 1600, def: 560, key: "rx-dock-w", css: "--dock-open", col: "--dock-w" };

const STANDARD_DOCK_MAX = 880;
export const dockMax = (room: number) => Math.min(DOCK.max, Math.max(STANDARD_DOCK_MAX, room / 2));

const clamp = (v: number, lo: number, hi: number) => Math.min(hi, Math.max(lo, v));

// How far the handle may give at the limit. The width itself stays clamped: the
// constraint is real and should not be faked. What gives is the handle, and it
// says "this is the edge", not "there is more room".
const OVER = 9;
const give = (d: number) => OVER * Math.tanh(d / 90);

// How much further you have to push, past that give, for the panel to close —
// and the same pull the other way to bring it back. The give is what makes the
// threshold safe: a slip stops at the edge, and only sustained force commits.
const COMMIT = 56;

export function widthOf(span: Span): number {
  const saved = Number(localStorage.getItem(span.key));
  return Number.isFinite(saved) && saved > 0 ? clamp(saved, span.min, span.max) : span.def;
}

/** The other half of widthOf: what a released drag commits, written beside the
 *  read so one place spells the key. */
export function keepWidth(span: Span, w: number) {
  localStorage.setItem(span.key, String(Math.round(w)));
}

interface Props {
  edge: "l" | "r";
  span: Span;
  width: number;
  max?: number;
  label: string;
  open: boolean;
  onWidth: (w: number) => void;
  onOpen: (v: boolean) => void;
}

export function Gutter({ edge, span, width, max = span.max, label, open, onWidth, onOpen }: Props) {
  const held = useRef(0);

  const drag = (e: ReactPointerEvent<HTMLDivElement>) => {
    if (e.button !== 0) return;
    const bar = e.currentTarget;
    const app = bar.closest(".app") as HTMLElement | null;
    if (!app) return;
    e.preventDefault();

    const x0 = e.clientX;
    const wasShut = !open;
    let now = open ? width : span.min;
    let shut = wasShut;
    let over = 0;
    let raf = 0;
    bar.setPointerCapture(e.pointerId);
    bar.dataset.on = "";
    app.dataset.drag = "pointer";

    // 每帧把宽度直接写进变量，而不是走一次 setState：栏宽是拖着的手在感觉的
    // 东西，而这棵树上还挂着一条正在流的转录。写的是列宽那个变量而不是记住的
    // 那个 —— 关到一半时两者必须能分开。
    const paint = () => {
      app.style.setProperty(span.col, `${shut ? 0 : now}px`);
      bar.style.setProperty("--over", `${over}px`);
      raf = requestAnimationFrame(paint);
    };
    const move = (ev: PointerEvent) => {
      const dx = edge === "l" ? ev.clientX - x0 : x0 - ev.clientX;
      // Positive means widening; the screen direction is applied once at the end
      // so the right column, which widens as the pointer moves left, reads the
      // same way here.
      let pull = 0;
      if (wasShut) {
        shut = dx < COMMIT;
        now = shut ? span.min : clamp(dx, span.min, max);
        pull = shut ? give(Math.max(0, dx)) : 0;
      } else {
        const want = width + dx;
        if (want >= span.min) {
          shut = false;
          now = Math.min(max, want);
          pull = want > max ? give(want - max) : 0;
        } else {
          shut = span.min - want > COMMIT;
          now = span.min;
          pull = shut ? 0 : -give(span.min - want);
        }
      }
      over = edge === "l" ? pull : -pull;
    };
    const up = () => {
      cancelAnimationFrame(raf);
      bar.removeEventListener("pointermove", move);
      bar.removeEventListener("pointerup", up);
      bar.removeEventListener("pointercancel", up);
      delete bar.dataset.on;
      bar.style.setProperty("--over", "0px");
      // 最后一次 move 之后未必还有一帧，栏会停在比松手位置差几像素的地方。这
      // 几像素得在补间恢复之前就落定，否则它们会被慢慢走完 —— 松手时的一记回
      // 弹。读一次布局，就是让这个值先以「不补间」的身份定下来。
      app.style.setProperty(span.col, `${shut ? 0 : now}px`);
      void app.offsetWidth;
      delete app.dataset.drag;
      // Held one frame longer than the release: the stylesheet only produces
      // this same width once React has landed the open/closed attribute, and
      // clearing early flashes the column it had before the drag.
      requestAnimationFrame(() => app.style.removeProperty(span.col));
      if (shut === open) onOpen(!shut);
      if (!shut) onWidth(now);
    };
    bar.addEventListener("pointermove", move);
    bar.addEventListener("pointerup", up);
    bar.addEventListener("pointercancel", up);
    paint();
  };

  const toggle = () => onOpen(!open);
  const rail = edge === "l";

  const key = (e: ReactKeyboardEvent<HTMLDivElement>) => {
    // The pointer commits by pushing through; the keyboard says it outright.
    if (e.key === "Enter" || e.key === " ") {
      e.preventDefault();
      toggle();
      return;
    }
    const dir = e.key === "ArrowLeft" ? -1 : e.key === "ArrowRight" ? 1 : 0;
    if (dir) {
      if (!open) return;
      e.preventDefault();
      const bar = e.currentTarget;
      const next = clamp(width + dir * (e.shiftKey ? 48 : 12) * (edge === "l" ? 1 : -1), span.min, max);
      // At the limit. The drag has its rubber band; without one here the keyboard
      // just stops responding, which is indistinguishable from a broken key.
      if (next === width) {
        bar.style.setProperty("--bump", `${dir * 5}px`);
        delete bar.dataset.edge;
        void bar.offsetWidth;   // the attribute must leave first, or it will not replay
        bar.dataset.edge = "";
        return;
      }
      // 一步 12px 配一段 .34s 的补间，连按就成了追不上的橡皮筋。按住时不补间，
      // 停手后补间归位 —— 双击复位那种整段位移仍然该被看见。
      const app = bar.closest(".app") as HTMLElement | null;
      if (app) {
        app.dataset.drag = "key";
        clearTimeout(held.current);
        held.current = window.setTimeout(() => delete app.dataset.drag, 220);
      }
      onWidth(next);
    } else if (e.key === "Home" && open) {
      e.preventDefault();
      onWidth(span.def);
    }
  };

  return (
    <div
      className={`gutter gutter-${edge}`}
      role="separator"
      aria-orientation="vertical"
      aria-label={label}
      aria-valuenow={open ? Math.round(width) : 0}
      aria-valuemin={0}
      aria-valuemax={max}
      data-shut={open ? undefined : ""}
      tabIndex={0}
      onPointerDown={drag}
      onDoubleClick={() => open && onWidth(span.def)}
      onKeyDown={key}
      data-action-keydown={rail ? "chrome.rail" : "browser.open"}
    >
      {/* The one control for both directions, sitting where the hand already is.
          Not a tab stop of its own — Enter on the separator does the same thing,
          and a second stop per divider buys nothing. Two marks, cross-faded: the
          divider's own gesture is dragging, so at rest it shows a handle, and
          the arrow — what a click would do — arrives with the hand. */}
      <button
        className="grip"
        tabIndex={-1}
        aria-hidden="true"
        title={open ? t("收起") : t("展开")}
        onPointerDown={(e) => e.stopPropagation()}
        onClick={toggle}
        data-action-click={rail ? "chrome.rail" : "browser.open"}
      >
        <i className="knurl" aria-hidden="true" />
        <span className="dir" aria-hidden="true">{(edge === "l") === open ? "‹" : "›"}</span>
      </button>
    </div>
  );
}
