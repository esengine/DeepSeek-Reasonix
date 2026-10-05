import { useEffect, useRef, useState, type RefObject } from "react";
import { t } from "../../i18n";
import { reason } from "../../i18n/kernel";
import type { AgentPort, ContextBreakdown } from "../../port/port";
import { pct as percent, tokens } from "../../i18n/format";
import { pinToViewport } from "../place";
import { useFoldBound } from "./useFoldBound";
import { Row } from "./kit";

// The order is the order they arrive in a prompt, so the bar reads the way the
// request is built rather than by size — a class that grows is easier to spot
// when its neighbours stay put.
//
// Read at render, not at import. t() answers out of a dictionary boot() installs,
// and a module body runs before it — so a table built here froze five labels in
// the source language and never followed the interface into English again. The
// literals stay inside t() so the catalogue scanner still sees them.
// The five message classes, named rather than "some key of the breakdown":
// the type also carries the two ceilings and the boundary's name, and none of
// those is a slice of the bar.
type Part = "system" | "tools" | "user" | "reply" | "output";

function parts(): [Part, string, string][] {
  return [
    ["system", t("系统提示"), t("基础指令、记忆、技能清单")],
    ["tools", t("工具定义"), t("发给模型的工具清单")],
    ["user", t("你的输入"), t("本会话中你输入的部分")],
    ["reply", t("模型回复"), t("模型的输出")],
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
 *
 *  Usage is one number and it is drawn once. The two ceilings under it are two
 *  questions, not two gauges: the fold point is a deadline — the bar is drawn
 *  against it, because against a 1M window the default soft limit fires at 16%
 *  of it and a gauge drawn the other way reads nearly empty at the moment
 *  maintenance happens — while the window says what the model could hold at
 *  all. Each names its own denominator, so neither becomes a percentage of
 *  something unstated, and the window stays the number a relay gets wrong and
 *  the only place it can be corrected. */
export function Context({ ctx, legend = false, port, onCtx }: {
  ctx: ContextBreakdown | null;
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
  const [open, setOpen] = useState(false);
  // The two ceilings are edited in the same column, so opening one closes the
  // other: side by side they would be two boxes of numbers with no way to tell
  // which figure each answers for.
  const [editing, setEditing] = useState(false);
  const [tuning, setTuning] = useState(false);
  const bar = useRef<HTMLDivElement>(null);
  // The handle is the capacity bar's own mark, so its state lives here rather
  // than in a control mounted beside it: an editor that owned the track would
  // rebuild it on every open, and a bar remounted animates from zero, which
  // reads as the session's usage jumping.
  const fold = useFoldBound(port, ctx?.window ?? 0, onCtx);

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
  // The kernel's figure, drawn as it arrived. It moves once per request, so
  // easing between two of them would put readings on screen that no request
  // ever produced.
  const used = ctx.used || 0;
  const settable = port && onCtx;
  const field = settable && (
    <DeclareWindow port={port} onSet={onCtx} was={ctx.window} onDone={() => setEditing(false)} />
  );
  const openWindow = () => {
    setEditing((v) => !v);
    setTuning(false);
  };
  // A window nobody declared has no denominator to draw against — but the
  // number still matters, and so does what else a zero window means: it is
  // what turns automatic compaction off. Vanishing said neither.
  if (!ctx.window) {
    const missing = (
      <>
        <Row k={t("上下文")} v={tokens(Math.round(used))} />
        <p className="ctxnote">
          {t("该来源未声明窗口大小，因此无法显示已用比例，也不会自动压缩。中转服务转发的是第三方模型，其容量只有你知道。")}
        </p>
        {field}
      </>
    );
    return legend ? <div className="block" data-b="ctx">{missing}</div> : missing;
  }
  // A fold point past the window is how maintenance is retired: the ratio was
  // placed where usage cannot reach it. There is no deadline left to count
  // down to, so the capacity row is the whole gauge rather than a footnote.
  const folds = ctx.compact_at > 0 && ctx.compact_at <= ctx.window;
  // While the handle is held, it owns the figure: a mark that moves under the
  // pointer while the number above it stays put is two controls disagreeing
  // about one setting.
  const at = fold.reading ?? ctx.compact_at;
  const denom = folds ? ctx.compact_at : ctx.window;
  const pct = Math.min((used / denom) * 100, 100);
  // The kernel's own two rungs, not a second opinion: it tells the model to
  // work narrower at 75% of the trigger and to land what it knows at 92%, and
  // a panel inventing its own thresholds would report a pressure the session
  // is not under. Neither is a fault — maintenance is routine — so they read
  // as accent, never as warn or error.
  const press = !folds ? undefined : pct >= 92 ? "soon" : pct >= 75 ? "near" : undefined;
  const shown = parts().map(([k, label, why]) => ({ k, label, why, n: ctx[k] || 0 })).filter((p) => p.n > 0);
  const sum = shown.reduce((a, p) => a + p.n, 0) || 1;

  // A relay's window is one number somebody typed for every model it forwards,
  // so the denominator is a guess as often as a fact — and this is the only
  // place it is read.
  const windowFigure = (
    <span className="ctxq">
      {settable ? (
        <button
          className="ctxden"
          aria-expanded={editing}
          title={t("该窗口值的来源无法确定 —— 点击可改为该模型的实际上限")}
          onClick={openWindow}
        >
          {tokens(ctx.window)}
        </button>
      ) : (
        tokens(ctx.window)
      )}
      <em>{percent(used / ctx.window)}</em>
    </span>
  );

  const body = (
    <>
      {/* One usage figure. The two ceilings under it answer different questions
          — when the host will tidy up, and when the model simply cannot hold
          any more — so both keep a row, and each names its own denominator
          rather than collapsing into one percentage of something unstated. */}
      <Row k={t("上下文")} v={<span className="ctxq">{tokens(Math.round(used))}</span>} />
      {!folds && <Row k={t("模型容量")} v={windowFigure} />}
      {!folds && editing && field}
      <div
        className="ctxbar"
        data-press={press}
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
      {press && (
        <p className="ctxnote" data-press={press}>
          {press === "soon"
            ? t("即将到达维护点，随后会自动整理上下文。")
            : t("接近维护点，模型已经被告知要收窄接下来的工作。")}
        </p>
      )}
      {/* Capacity is the diagnosis, not the deadline: it answers "is this model
          simply too small" and it is where a wrong window is corrected. */}
      {folds && (
        <div className="ctxcap">
          {/* The fold point is a setting, and this is where it is read — so it
              is also where it is changed. Behind a settings sheet it is found
              only by readers who already knew it was there, which is not the
              reader this footnote was written for. */}
          <Row
            k={t("下次维护")}
            v={
              <span className="ctxq">
                {settable ? (
                  <button
                    className="ctxden"
                    data-action="compaction.advanced"
                    aria-expanded={tuning}
                    title={t("点这里改维护点")}
                    onClick={() => {
                      setTuning((v) => !v);
                      setEditing(false);
                    }}
                  >
                    {tokens(at)}
                  </button>
                ) : (
                  tokens(at)
                )}
                <em>{percent(used / at)}</em>
              </span>
            }
          />
          <Row k={t("模型容量")} v={windowFigure} />
          {/* The fold point marked on the window it is a fraction of. Read as
              two numbers, 160k against 1M is arithmetic nobody does; read as a
              notch this far along the bar, it is the whole answer at a glance. */}
          <div className="ctxcapbar" data-live={tuning || undefined}>
            <i style={{ width: `${Math.min((used / ctx.window) * 100, 100)}%` }} />
            <b style={{ left: `${Math.min((at / ctx.window) * 100, 100)}%` }} />
            {/* The mark, made to move. A native range brings the keyboard, the
                touch target and the announced value with it; everything drawn
                here is the bar that was already on screen, so the control the
                reader grabs is the one they were already reading.
                The track spans the window because the bar does: given its own
                min..max the handle sat at one fraction of the track while the
                mark sat at another, and the two drifted apart by the width of
                everything the track had cut off. */}
            {settable && tuning && fold.ready && (
              <input
                className="ctxslide"
                type="range"
                min={fold.step}
                max={ctx.window}
                step={fold.step}
                value={Math.min(Math.max(fold.preview ?? ctx.compact_at, fold.step), ctx.window)}
                disabled={fold.busy}
                autoFocus
                data-action-change="compaction.threshold"
                data-action-pointerup="compaction.threshold"
                data-action-keyup="compaction.threshold"
                aria-label={t("维护点")}
                aria-valuetext={tokens(at)}
                onChange={(e) => fold.move(Number(e.currentTarget.value))}
                onPointerUp={(e) => void fold.commit(Number(e.currentTarget.value))}
                onKeyUp={(e) => {
                  if (e.key === "Escape") return setTuning(false);
                  void fold.commit(Number(e.currentTarget.value));
                }}
              />
            )}
          </div>
          {/* Which bound is holding, said only where it is not self-evident: a
              fold at the window's own share explains itself, and a fold at a
              fixed size against a window twenty times larger does not. It
              stands down while the editor is open — the chosen mode says what
              it does there, and two greyed paragraphs stacked on one column
              read as one nobody finishes. */}
          {ctx.boundary === "economic" && ctx.capacity_at > ctx.compact_at && !tuning && (
            <p className="ctxwhy">
              {t("维护点是固定输入量，不随窗口放大 —— 输入越大，每轮越慢。窗口那条线在 {n}。", {
                n: tokens(ctx.capacity_at),
              })}
            </p>
          )}
          {settable && tuning && fold.ready && (
            <div className="ctxfold">
              {fold.error && <p className="ctxnote" data-lvl="warn">{fold.error}</p>}
              {/* Which of the three the handle is expressing, and where the
                  other end is — one line, because a control that explains
                  itself in a paragraph has stopped being a control. */}
              <p className="ctxnote">
                {fold.preview !== null
                  ? fold.preview >= fold.capacity
                    ? t("松开即按窗口容量")
                    : t("松开即生效")
                  : fold.mode === "capacity"
                    ? t("按窗口容量（默认）· {n}", { n: tokens(fold.capacity) })
                    : t("自定义 {n} · 拖到最右恢复按窗口容量", { n: tokens(fold.stored) })}
              </p>
              <p className="ctxfine">{t("会重建运行时；任务运行中改不了。")}</p>
            </div>
          )}
        </div>
      )}
      {folds && editing && field}
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
          <p className="foot">{t("估算值，与触发压缩使用同一口径")}</p>
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
          {t("保存")}
        </button>
      </div>
      {error && <p className="ctxnote" data-lvl="warn">{error}</p>}
      <p className="ctxnote">
        {t(was > 0
          ? "只改当前这个模型，同一个来源下的其它模型不动。填模型文档写的上下文上限，不是最大输出。会重建运行时，任务跑着的时候改不了。"
          : "填写模型文档中的上下文上限，而非最大输出长度。将重建运行时，任务运行期间无法修改。")}
      </p>
    </div>
  );
}
