import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { useStartsOpen } from "../../state/foldpref";
import { StudioIcon } from "../StudioIcon";
import { t } from "../../i18n";
import { count, decimals } from "../../i18n/format";
import { Sym } from "../Sym";
import type { Item } from "../../state/session";
import { thoughtStartedAt } from "../../state/say";
import { LazyMarkdown } from "../LazyMarkdown";
import { Boundary } from "../Boundary";
import { CopyButton } from "../CopyButton";
import { useRevealed } from "../reveal";
import { useShown } from "../shown";
import { ReplyMenu } from "./ReplyMenu";
import { useDismiss } from "../dismiss";
import { pinToViewport } from "../place";

// Folded, the only thing left of a thought is how much of the turn it was. The
// spec puts both halves there — how long, and how much — because either alone
// hides whether a slow turn was spent thinking or waiting.
function ThoughtLabel({ item }: { item: Extract<Item, { t: "say" }> }) {
  // Graphemes, not code units: an emoji in the reasoning is one character to
  // the reader and two to the string.
  const graphemes = useMemo(() => [...(item.reasoning ?? "")].length, [item.reasoning]);
  const chars = t("{n} 字", { n: count(graphemes) });
  return <>{item.thoughtMs ? t("思考 {secs} 秒 · {chars}", { secs: decimals(item.thoughtMs / 1000, 1), chars }) : t("思考 {chars}", { chars })}</>;
}

const LIVE_TICK_MS = 100;

// While the model is still thinking the reader sees the clock run, not a bare
// ellipsis; a card this window did not watch begin has no start to count from.
function LiveThought({ id }: { id: string }) {
  const since = thoughtStartedAt(id);
  const watched = useShown();
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (since === undefined || !watched) return;
    setNow(Date.now());
    const h = window.setInterval(() => setNow(Date.now()), LIVE_TICK_MS);
    return () => window.clearInterval(h);
  }, [since, watched]);
  if (since === undefined) return <>{t("思考中…")}</>;
  return <>{t("思考中 {secs} 秒", { secs: decimals(Math.max(0, now - since) / 1000, 1) })}</>;
}

// A ref is "provider/model"; the model is what identifies it to a reader, and
// the provider stays in the title for when two of them carry the same name.
const modelName = (ref: string) => ref.slice(ref.lastIndexOf("/") + 1);

/** A quote on its way to the composer. n makes the same text twice two
 *  requests; turn is the kernel's name for the reply it came from, absent when
 *  the transcript was rebuilt without checkpoints. */
export interface Quote {
  text: string;
  turn?: number;
  n: number;
}

/** What a finished reply can be acted on with. Regeneration belongs to the
 *  individual reply and requires a paired checkpoint. */
export interface ReplyActions {
  onQuote: (text: string, id: string) => void;
  canRegenerate: (id: string) => boolean;
  hasLaterTurns: (id: string) => boolean;
  onRegenerate: (id: string) => void;
  model?: string;
  onConfigureModel?: () => void;
  onRunDetail?: () => void;
}

// The browser keeps selections outside the answer, including across cards.
function selectedIn(card: Element | null): string {
  const sel = card && window.getSelection();
  if (!sel || sel.isCollapsed || sel.rangeCount !== 1) return "";
  const range = sel.getRangeAt(0);
  if (!card.contains(range.commonAncestorContainer)) return "";
  return sel.toString().trim();
}

function download(text: string) {
  const url = URL.createObjectURL(new Blob([text], { type: "text/markdown;charset=utf-8" }));
  const link = document.createElement("a");
  link.href = url;
  link.download = "reasonix-reply.md";
  link.click();
  URL.revokeObjectURL(url);
}

export function SayCard({ item, afterAnswer, reply }: { item: Extract<Item, { t: "say" }>; afterAnswer?: ReactNode; reply?: ReplyActions }) {
  const start = useStartsOpen("thinking", !item.done && !item.text);
  const [touched, setOpen] = useState<boolean | null>(null);
  const open = touched ?? start;
  const [menu, setMenu] = useState<"" | "retry" | "more">("");
  const shut = useCallback(() => setMenu(""), []);
  const retry = useRef<HTMLButtonElement>(null);
  const more = useRef<HTMLButtonElement>(null);
  const answer = useRef<HTMLDivElement>(null);
  const selectionMenu = useRef<HTMLDivElement>(null);
  const [selection, setSelection] = useState<{ text: string; x: number; y: number } | null>(null);
  const closeSelection = useCallback(() => {
    if (selectionMenu.current?.contains(document.activeElement)) answer.current?.focus();
    setSelection(null);
  }, []);
  useDismiss(!!selection, selectionMenu, closeSelection);
  const offerSelection = (x: number, y: number) => {
    if (!item.done || !reply) return false;
    const text = selectedIn(answer.current);
    if (!text) return false;
    if (x === 0 && y === 0) {
      const rect = window.getSelection()!.getRangeAt(0).getBoundingClientRect();
      x = rect.left;
      y = rect.bottom;
    }
    setMenu("");
    setSelection({ text, x, y });
    return true;
  };
  // Thinking is the longest-running stream of the turn — 10s of it before the
  // first answer token, measured — so it gets the same paced reveal the answer
  // does rather than tracking the wire's bursts.
  const thought = useRevealed(item.reasoning ?? "", !item.done && open);
  return (
    <div className="call" data-k="say">
      <div className="g">
        <Sym glyph="◦" />
        <span className="line" />
      </div>
      <div className="c">
        <div className="hl">
          <span className="nm">reasonix</span>
          {/* Which model wrote this one. A turn can carry two, and without it
              both read as whatever the composer happens to name now. */}
          {item.model && <span className="src" title={item.model}>{modelName(item.model)}</span>}
        </div>
        <div className="out">
          {item.reasoning?.trim() && (
            <details className="think" open={open} onToggle={(e) => e.currentTarget.open !== open && setOpen(e.currentTarget.open)}>
              <summary>
                <span className="fold">{item.done || item.thoughtMs !== undefined ? <ThoughtLabel item={item} /> : <LiveThought id={item.id} />}</span>
              </summary>
              <div className="tk">
                {thought}
                {!item.done && <span className="caret" />}
              </div>
            </details>
          )}
          {item.text && (
            <div className="txt" ref={answer} tabIndex={item.done && reply ? 0 : undefined}
              data-action-contextmenu="reply.selection.menu" data-action-keydown="reply.selection.menu" data-target={item.id}
              onContextMenu={(e) => {
                if (!offerSelection(e.clientX, e.clientY)) return;
                e.preventDefault();
                e.stopPropagation();
              }}
              onKeyDown={(e) => {
                if (e.key !== "ContextMenu" && !(e.key === "F10" && e.shiftKey)) return;
                if (!offerSelection(0, 0)) return;
                e.preventDefault();
                e.stopPropagation();
              }}>
              <Boundary retryKey={item.text} fallback={<div className="md" style={{ whiteSpace: "pre-wrap" }}>{item.text}</div>}>
                <LazyMarkdown text={item.text} streaming={!item.done} />
              </Boundary>
            </div>
          )}
          {afterAnswer}
          {/* Only once the answer is whole: copying half a stream hands over
              something that was never said. */}
          {item.done && item.text.trim() && (
            <div className="acts">
              <CopyButton text={item.text} iconOnly />
              {reply && (
                <button type="button" data-action="reply.quote" data-target={item.id} title={t("引用到输入框")} aria-label={t("引用到输入框")} onClick={() => reply.onQuote(selectedIn(answer.current) || item.text, item.id)}>
                  <StudioIcon name="quote" />
                </button>
              )}
              {reply?.canRegenerate(item.id) && (
                <span className="acts-menu">
                  <button ref={retry} type="button" data-action="reply.retry" title={t("重新生成")} aria-label={t("重新生成")} aria-expanded={menu === "retry"} onClick={() => setMenu((m) => (m === "retry" ? "" : "retry"))}>
                    <StudioIcon name="refresh" />
                  </button>
                  <ReplyMenu anchor={retry} open={menu === "retry"} onClose={shut}>
                    <div className="acts-pop-head">
                      {t("重新生成")}
                      <small>{t("当前回复会留在运行历史里")}</small>
                      {reply.hasLaterTurns(item.id) && <small>{t("这一轮之后的记录会被丢弃")}</small>}
                    </div>
                    <button type="button" role="menuitem" data-action="reply.retry-now" onClick={() => { setMenu(""); reply.onRegenerate(item.id); }}>
                      <StudioIcon name="refresh" /><span>{t("按当前配置重试")}</span>{reply.model && <small>{reply.model}</small>}
                    </button>
                    {reply.onConfigureModel && (
                      <button type="button" role="menuitem" data-action="reply.configure" onClick={() => { setMenu(""); reply.onConfigureModel?.(); }}>
                        <StudioIcon name="sliders" /><span>{t("先调整模型与强度")}</span>
                      </button>
                    )}
                  </ReplyMenu>
                </span>
              )}
              {reply && (
                <span className="acts-menu">
                  <button ref={more} type="button" data-action="reply.more" title={t("更多")} aria-label={t("更多")} aria-expanded={menu === "more"} onClick={() => setMenu((m) => (m === "more" ? "" : "more"))}>
                    <StudioIcon name="more" />
                  </button>
                  <ReplyMenu anchor={more} open={menu === "more"} onClose={shut}>
                    <div className="acts-pop-head">{t("这条回复")}</div>
                    <button type="button" role="menuitem" data-action="reply.download" onClick={() => { setMenu(""); download(item.text); }}>
                      <StudioIcon name="download" /><span>{t("下载回复")}</span><small>Markdown</small>
                    </button>
                    {reply.onRunDetail && (
                      <>
                        <div className="acts-pop-head">{t("本轮执行")}</div>
                        <button type="button" role="menuitem" data-action="reply.run-detail" onClick={() => { setMenu(""); reply.onRunDetail?.(); }}>
                          <StudioIcon name="gauge" /><span>{t("运行分析")}</span>
                        </button>
                      </>
                    )}
                  </ReplyMenu>
                </span>
              )}
            </div>
          )}
        </div>
      </div>
      {selection && reply && createPortal(
        <div className="acts-pop" role="menu" aria-label={t("选中内容")} data-action-keydown="reply.selection.menu" data-target={item.id} ref={(el) => {
          selectionMenu.current = el;
          if (el) pinToViewport(el, selection.x, selection.y);
        }}
          onKeyDown={(e) => {
            const keys = ["ArrowDown", "ArrowUp", "Home", "End"];
            if (!keys.includes(e.key)) return;
            const all = [...e.currentTarget.querySelectorAll<HTMLElement>('[role="menuitem"]')];
            const at = all.indexOf(document.activeElement as HTMLElement);
            const next = e.key === "Home" ? 0 : e.key === "End" ? all.length - 1 : (at + (e.key === "ArrowDown" ? 1 : -1) + all.length) % all.length;
            e.preventDefault();
            all[next]?.focus();
          }}>
          <button type="button" role="menuitem" autoFocus data-action="reply.quote" data-target={item.id} onClick={() => {
            setSelection(null);
            reply.onQuote(selection.text, item.id);
          }}><span>{t("引用选中内容")}</span></button>
          <CopyButton text={selection.text} role="menuitem" label={t("复制选中内容")} />
        </div>, document.body,
      )}
    </div>
  );
}
