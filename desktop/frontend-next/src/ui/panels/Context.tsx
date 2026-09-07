import { useEffect, useRef, useState, type RefObject } from "react";
import { t } from "../../i18n";
import { reason } from "../../i18n/kernel";
import { useTicker } from "../num";
import type { AgentPort, ContextBreakdown } from "../../port/port";
import { pct as percent, tokens } from "../../i18n/format";
import { pinToViewport } from "../place";
import { Row } from "./kit";

// The order is the order they arrive in a prompt, so the bar reads the way the
// request is built rather than by size — a class that grows is easier to spot
// when its neighbours stay put.
//
// Read at render, not at import. t() answers out of a dictionary boot() installs,
// and a module body runs before it — so a table built here froze five labels in
// the source language and never followed the interface into English again. The
// literals stay inside t() so the catalogue scanner still sees them.
function parts(): [keyof ContextBreakdown, string, string][] {
  return [
    ["system", t("系统提示"), t("基础指令、记忆、技能清单")],
    ["tools", t("工具定义"), t("发给模型的工具清单")],
    ["user", t("你说的话"), t("这一会话里你输入的部分")],
    ["reply", t("模型回复"), t("模型说过的话")],
    ["output", t("工具输出"), t("命令、读取、检索返回的内容")],
  ];
}


// Gap between the bar and the bubble. It lives here rather than in CSS because
// a fixed bubble is placed by measurement, so no rule owns the offset any more.
const GAP = 9;

// The metrics rail scrolls, and a scroller clips: an absolutely positioned
// bubble lost its left edge to the middle pane as soon as the rail was dragged
// narrower than the bubble. Fixed lifts it out of the scroller; the size stays
// CSS's, read back off the element so no measurement is copied into JS.
function place(anchor: RefObject<HTMLElement | null>) {
  return (el: HTMLDivElement | null) => {
    if (!el || !anchor.current) return;
    const to = anchor.current.getBoundingClientRect();
    const box = el.getBoundingClientRect();
    const above = to.top - box.height - GAP;
    pinToViewport(el, to.right - box.width, above >= 6 ? above : to.bottom + GAP);
  };
}

/** Context is the gauge plus what fills it. The gauge alone says a session is
 *  at 70% without saying whether that is a tool catalogue, a memory file, or
 *  one enormous output — and those are fixed in completely different ways. The
 *  breakdown stays folded because it is a diagnosis, not a running number.
 *  row=false leaves the figure to the head card and captions the gauge with
 *  what it is instead — a share of the window, not the same number again. */
export function Context({ ctx, row = true, legend = false, port, onCtx }: {
  ctx: ContextBreakdown | null;
  row?: boolean;
  legend?: boolean;
  // Both are needed to offer the missing window: one to declare it, one to
  // carry the rebuilt gauge back. Without them the panel still says why it
  // cannot draw, which is the half that was there before.
  port?: AgentPort;
  onCtx?: (next: ContextBreakdown) => void;
}) {
  // Every hook runs before the first return: ctx arrives one render after the
  // rail mounts, and a guard above them made that render ask for hooks the
  // previous one never did.
  const used = useTicker(ctx?.used || 0);
  const [open, setOpen] = useState(false);
  const [editing, setEditing] = useState(false);
  const bar = useRef<HTMLDivElement>(null);

  // A bubble placed once against the viewport goes stale the moment anything
  // moves it, and there is nothing useful to show mid-scroll — so it closes.
  useEffect(() => {
    if (!open) return;
    const close = () => setOpen(false);
    addEventListener("scroll", close, true);
    addEventListener("resize", close);
    return () => {
      removeEventListener("scroll", close, true);
      removeEventListener("resize", close);
    };
  }, [open]);

  if (!ctx) return null;
  const settable = port && onCtx;
  const field = settable && (
    <DeclareWindow port={port} onSet={onCtx} was={ctx.window} onDone={() => setEditing(false)} />
  );
  // A window nobody declared has no denominator to draw against — but the
  // number still matters, and so does what else a zero window means: it is
  // what turns automatic compaction off. Vanishing said neither.
  if (!ctx.window) {
    const missing = (
      <>
        <Row k={t(row ? "上下文窗口" : "上下文构成")} v={tokens(Math.round(used))} />
        <p className="ctxnote">
          {t("没人说过这个来源的窗口有多大，所以画不出用了多少 —— 也不会自动压缩。中转站转发的是别人的模型，只有你知道它有多大。")}
        </p>
        {field}
      </>
    );
    return legend ? <div className="block" data-b="ctx">{missing}</div> : missing;
  }
  const pct = Math.min((used / ctx.window) * 100, 100);
  const shown = parts().map(([k, label, why]) => ({ k, label, why, n: ctx[k] || 0 })).filter((p) => p.n > 0);
  const sum = shown.reduce((a, p) => a + p.n, 0) || 1;

  const body = (
    <>
      <Row
        k={t(row ? "上下文窗口" : "上下文构成")}
        v={
          // One span, because .drow .v spaces its children: the two halves of
          // "24.8k / 200k" are one figure, not two gauges.
          <span className="ctxq">
            {tokens(Math.round(used))} /{" "}
            {/* A relay's window is one number somebody typed for every model it
                forwards, so the denominator is a guess as often as a fact — and
                here is the only place it is read. */}
            {settable ? (
              <button
                className="ctxden"
                aria-expanded={editing}
                title={t("这个窗口是谁填的说不准 —— 点一下改成这个模型真正的上限")}
                onClick={() => setEditing((v) => !v)}
              >
                {tokens(ctx.window)}
              </button>
            ) : (
              tokens(ctx.window)
            )}
          </span>
        }
      />
      {editing && field}
      <div
        className="ctxbar"
        ref={bar}
        tabIndex={0}
        role="group"
        aria-label={t("上下文构成")}
        onMouseEnter={() => setOpen(true)}
        onMouseLeave={() => setOpen(false)}
        onFocus={() => setOpen(true)}
        onBlur={() => setOpen(false)}
      >
        {shown.map((p) => (
          <i key={p.k} data-p={p.k} style={{ width: `${(p.n / sum) * pct}%` }} />
        ))}
      </div>
      {legend && (
        <div className="ctxlg">
          {shown.map((p) => (
            <div className="r" key={p.k} title={p.why}>
              <i data-p={p.k} />
              <span className="t">{p.label}</span>
              <em>{percent(p.n / sum)}</em>
              <b>{tokens(p.n)}</b>
            </div>
          ))}
        </div>
      )}
      {open && !legend && (
        <div className="ctxpop" role="tooltip" ref={place(bar)}>
          <div className="hd">
            <span>{t("上下文构成")}</span>
            <span className="n">{percent(pct / 100)}</span>
          </div>
          {shown.map((p) => (
            <div className="row" key={p.k} title={p.why}>
              <i data-p={p.k} />
              <span className="t">{p.label}</span>
              <span className="v">{tokens(p.n)}</span>
              <span className="p">{percent(p.n / sum)}</span>
            </div>
          ))}
          <p className="foot">{t("估算值，和触发压缩用的是同一把尺子")}</p>
        </div>
      )}
    </>
  );
  return legend ? <div className="block" data-b="ctx">{body}</div> : body;
}

/** The one number nobody can probe. A relay forwards a third party's model under
 *  its own name, so the endpoint reports no window and the catalogue has no row
 *  for it. The field is offered against a declared window too, and not only a
 *  missing one: an endpoint-wide window is one figure somebody typed for every
 *  model behind that key, which makes it wrong for all but the one it was typed
 *  against. It is written per model, so correcting it here speaks for this
 *  model alone. */
function DeclareWindow({ port, onSet, was, onDone }: {
  port: AgentPort;
  onSet: (next: ContextBreakdown) => void;
  was: number;
  onDone: () => void;
}) {
  // Prefilled, because the common edit is to a figure that is already there:
  // an empty box asks for the whole number again to change one digit of it.
  const [draft, setDraft] = useState(was > 0 ? String(was) : "");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  const commit = async () => {
    const window = Number(draft);
    if (!window || busy) return;
    setBusy(true);
    setError("");
    try {
      onSet(await port.setContextWindow(window));
      onDone();
    } catch (e) {
      setError(reason(e));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="ctxwin">
      <div className="r">
        <input
          autoFocus={was > 0}
            data-action-keydown="context.window-tokens"
          inputMode="numeric"
          value={draft}
          disabled={busy}
          placeholder="131072"
          aria-label={t("上下文窗口（tokens）")}
          onChange={(e) => setDraft(e.target.value.replace(/\D/g, ""))}
          onKeyDown={(e) => {
            if (e.key === "Enter") void commit();
            if (e.key === "Escape") onDone();
          }}
        />
        <button data-action="context.window-tokens" disabled={busy || !draft || Number(draft) === was} onClick={() => void commit()}>
          {t("记下")}
        </button>
      </div>
      {error && <p className="ctxnote" data-lvl="warn">{error}</p>}
      <p className="ctxnote">
        {t(was > 0
          ? "只改当前这个模型，同一个来源下的其它模型不动。填模型文档写的上下文上限，不是最大输出。会重建运行时，任务跑着的时候改不了。"
          : "填模型文档写的上下文上限，不是最大输出。会重建运行时，任务跑着的时候改不了。")}
      </p>
    </div>
  );
}
