import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { useThinkingPreview, useStartsOpen, useFoldModes } from "../../state/foldpref";
import { StudioIcon } from "../StudioIcon";
import { t } from "../../i18n";
import { count, decimals } from "../../i18n/format";
import { Sym } from "../Sym";
import type { Item } from "../../state/session";
import { thoughtStartedAt } from "../../state/say";
import { useThoughtFold } from "../thoughtfold";
import { LazyMarkdown } from "../LazyMarkdown";
import { Boundary } from "../Boundary";
import { CopyButton } from "../CopyButton";
import { TooltipButton } from "../TooltipButton";
import { useRevealed } from "../reveal";
import { useShown } from "../shown";
import { ReplyMenu } from "./ReplyMenu";
import { useDismiss } from "../dismiss";
import { pinToViewport } from "../place";
import { copySelection, SelectionBar } from "../SelectionBar";
import { useSelectionBar } from "../useSelectionBar";
import type { LocalRefs } from "../Markdown";

// Folded, the only thing left of a thought is how much of the turn it was. The
// spec puts both halves there — how long, and how much — because either alone
// hides whether a slow turn was spent thinking or waiting.
function ThoughtLabel({ item }: { item: Extract<Item, { t: "say" }> }) {
  // Graphemes, not code units: an emoji in the reasoning is one character to
  // the reader and two to the string.
  const graphemes = useMemo(() => [...(item.reasoning ?? "")].length, [item.reasoning]);
  const chars = t("chat.n_chars", { n: count(graphemes) });
  const secs = decimals((item.thoughtMs ?? 0) / 1000, 1);
  if (item.cut) return <>{t(item.cut === "stopped" ? "chat.thought_stopped_s" : "chat.thought_failed_s", { secs })}</>;
  return <>{item.thoughtMs ? t("chat.thought_chars_secs_s", { secs, chars }) : t("chat.thought_chars", { chars })}</>;
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
  if (since === undefined) return <>{t("chat.thinking")}</>;
  return <>{t("chat.thinking_secs_s", { secs: decimals(Math.max(0, now - since) / 1000, 1) })}</>;
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
  // The workspace file the text was selected in, when it did not come from a reply.
  source?: string;
  n: number;
}

/** What a finished reply can be acted on with. Regeneration belongs to the
 *  individual reply and requires a paired checkpoint. */
export interface ReplyActions {
  files?: LocalRefs;
  activeFile?: string;
  onAddFile?: (reference: string) => void;
  openDeliveredFile?: (path: string) => Promise<void>;
  revealDeliveredFile?: (path: string) => Promise<void>;
  onQuote: (text: string, id: string) => void;
  onQuoteFile?: (text: string, source: string) => void;
  canRegenerate: (id: string) => boolean;
  hasLaterTurns: (id: string) => boolean;
  onRegenerate: (id: string) => void;
  canBranch: (id: string) => boolean;
  onBranch: (id: string) => void | Promise<void>;
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

export function SayCard({ item, afterAnswer, settled = true, reply, quiet }: { item: Extract<Item, { t: "say" }>; afterAnswer?: ReactNode; settled?: boolean; reply?: ReplyActions; quiet?: boolean }) {
  const [branching, setBranching] = useState(false);
  const thinking = !item.done && item.thoughtMs === undefined;
  const start = useStartsOpen("thinking", thinking && !item.text);
  const alwaysOpen = useFoldModes().thinking === "open";
  // The title changes once, when a thought this card watched finishes; a
  // thought drawn already finished is not that change happening.
  const watchedThinking = useRef(thinking);
  const [settledHere, setSettledHere] = useState(false);
  useEffect(() => {
    const fell = watchedThinking.current && !thinking;
    watchedThinking.current = thinking;
    if (!fell) return;
    setSettledHere(true);
    const id = setTimeout(() => setSettledHere(false), 240);
    return () => clearTimeout(id);
  }, [thinking]);
  const folds = useThoughtFold();
  const [touched, setOpen] = useState<boolean | null>(() => folds.get(item.id) ?? null);
  const open = touched ?? start;
  const peeks = useThinkingPreview() && !open;
  const peek = peeks ? item.reasoning?.trimStart() : "";
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
  const bar = useSelectionBar(answer, selectedIn);
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
    bar.clear();
    setSelection({ text, x, y });
    return true;
  };
  // Thinking is the longest-running stream of the turn — 10s of it before the
  // first answer token, measured — so it gets the same paced reveal the answer
  // does rather than tracking the wire's bursts.
  const thought = useRevealed(item.reasoning ?? "", !item.done && open);
  return (
    <div className="call" data-k="say" data-quiet={quiet ? "" : undefined}>
      <div className="g">
        <Sym icon="agent" />
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
            <details className="think" open={open} data-live={thinking ? "" : undefined} data-full={alwaysOpen ? "" : undefined} data-cut={item.cut}
              data-settled={settledHere ? "" : undefined} onToggle={(e) => {
                const next = e.currentTarget.open;
                if (next === open) return;
                setOpen(next);
                folds.set(item.id, next);
              }}>
              <summary>
                <span className="fold">
                  <span className="think-swap">
                    {settledHere && (
                      <span className="think-label think-out" aria-hidden="true">
                        {t("chat.thinking_secs_s", { secs: decimals((item.thoughtMs ?? 0) / 1000, 1) })}
                      </span>
                    )}
                    <span className={thinking ? "think-label think-sweep" : "think-label"} key={thinking ? "live" : "done"}>
                      {thinking ? <LiveThought id={item.id} /> : <ThoughtLabel item={item} />}
                    </span>
                  </span>
                </span>
                {item.cut && !open && <span className="think-note">{t(item.cut === "stopped" ? "chat.thought_stopped_note" : "chat.thought_failed_note")}</span>}
                {peek && !item.cut && <span className="thinking-preview">{peek}</span>}
              </summary>
              <div className="tk">
                <div className="tk-in">
                  {thought}
                  {thinking && <span className="caret" />}
                </div>
              </div>
            </details>
          )}
          {item.text && (
            <div className="txt" ref={answer} tabIndex={item.done && reply ? 0 : undefined}
              data-action-contextmenu="reply.selection.menu" data-action-keydown="reply.selection.menu" data-action-mouseup="reply.selection.bar" data-target={item.id}
              onMouseUp={() => { if (item.done && reply) bar.settle(); }}
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
              <Boundary retryKey={item.text} fallback={<div className="srcfb"><div className="srcfb-hd"><span className="srcfb-tag">{t("chat.shown_as_written")}</span><span>{t("chat.this_layout_did_not_show")}</span></div><div className="md srcfb-src">{item.text}</div></div>}>
                <LazyMarkdown text={item.text} streaming={!item.done} local={reply?.files} />
              </Boundary>
            </div>
          )}
          {afterAnswer}
          {/* Only once the answer is whole: copying half a stream hands over
              something that was never said, and a row of actions under work
              that is still running reads as the turn being over. */}
          {item.done && settled && item.text.trim() && (
            <div className="acts">
              <CopyButton text={item.text} iconOnly />
              {reply && (
                <TooltipButton type="button" data-action="reply.quote" data-target={item.id} title={t("chat.quote_into_the_composer")} aria-label={t("chat.quote_into_the_composer")} onClick={() => reply.onQuote(selectedIn(answer.current) || item.text, item.id)}>
                  <StudioIcon name="quote" />
                </TooltipButton>
              )}
              {reply?.canBranch(item.id) && (
                <TooltipButton type="button" data-action="reply.branch" title={t("chat.create_a_conversation_branch")} aria-label={t("chat.create_a_conversation_branch")} disabled={branching} onClick={() => { setBranching(true); void Promise.resolve(reply.onBranch(item.id)).finally(() => setBranching(false)); }}>
                  <StudioIcon name={branching ? "clock" : "fork"} />
                </TooltipButton>
              )}
              {reply?.canRegenerate(item.id) && (
                <span className="acts-menu">
                  <TooltipButton ref={retry} type="button" data-action="reply.retry" title={t("chat.regenerate")} aria-label={t("chat.regenerate")} aria-expanded={menu === "retry"} onClick={() => setMenu((m) => (m === "retry" ? "" : "retry"))}>
                    <StudioIcon name="refresh" />
                  </TooltipButton>
                  <ReplyMenu anchor={retry} open={menu === "retry"} onClose={shut}>
                    <div className="acts-pop-head">
                      {t("chat.regenerate")}
                      <small>{t("chat.the_current_reply_stays_in")}</small>
                      {reply.hasLaterTurns(item.id) && <small>{t("chat.everything_after_this_turn_is")}</small>}
                    </div>
                    <button type="button" role="menuitem" data-action="reply.retry-now" onClick={() => { setMenu(""); reply.onRegenerate(item.id); }}>
                      <StudioIcon name="refresh" /><span>{t("chat.retry_as_configured")}</span>{reply.model && <small>{reply.model}</small>}
                    </button>
                    {reply.onConfigureModel && (
                      <button type="button" role="menuitem" data-action="reply.configure" onClick={() => { setMenu(""); reply.onConfigureModel?.(); }}>
                        <StudioIcon name="sliders" /><span>{t("chat.adjust_the_model_and_effort")}</span>
                      </button>
                    )}
                  </ReplyMenu>
                </span>
              )}
              {reply && (
                <span className="acts-menu">
                  <TooltipButton ref={more} type="button" data-action="reply.more" title={t("common.more")} aria-label={t("common.more")} aria-expanded={menu === "more"} onClick={() => setMenu((m) => (m === "more" ? "" : "more"))}>
                    <StudioIcon name="more" />
                  </TooltipButton>
                  <ReplyMenu anchor={more} open={menu === "more"} onClose={shut}>
                    <div className="acts-pop-head">{t("chat.this_reply")}</div>
                    <button type="button" role="menuitem" data-action="reply.download" onClick={() => { setMenu(""); download(item.text); }}>
                      <StudioIcon name="download" /><span>{t("chat.download_the_reply")}</span><small>Markdown</small>
                    </button>
                    {reply.onRunDetail && (
                      <>
                        <div className="acts-pop-head">{t("chat.this_turn")}</div>
                        <button type="button" role="menuitem" data-action="reply.run-detail" onClick={() => { setMenu(""); reply.onRunDetail?.(); }}>
                          <StudioIcon name="activity" /><span>{t("common.run_analysis")}</span>
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
      {bar.at && reply && !selection && (
        <SelectionBar at={bar.at}>
          <button type="button" data-action-click="reply.quote" data-action-mousedown="reply.quote" data-target={item.id}
            onMouseDown={(e) => e.preventDefault()} onClick={() => { reply.onQuote(bar.at!.text, item.id); bar.clear(); }}>
            <StudioIcon name="quote" /><span>{t("common.quote_selected_text")}</span>
          </button>
          <button type="button" data-action-click="reply.selection.copy" data-action-mousedown="reply.selection.copy" data-target={item.id}
            onMouseDown={(e) => e.preventDefault()} onClick={() => { copySelection(bar.at!.text); bar.clear(); }}>
            <StudioIcon name="copy" /><span>{t("chat.copy_selected_text")}</span>
          </button>
        </SelectionBar>
      )}
      {selection && reply && createPortal(
        <div className="acts-pop" role="menu" aria-label={t("chat.selected_text")} data-action-keydown="reply.selection.menu" data-target={item.id} ref={(el) => {
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
          }}><span>{t("common.quote_selected_text")}</span></button>
          <CopyButton text={selection.text} role="menuitem" label={t("chat.copy_selected_text")} />
        </div>, document.body,
      )}
    </div>
  );
}
