import { createPortal } from "react-dom";
import { useCallback, useEffect, useLayoutEffect, useRef, useState, type CSSProperties } from "react";
import { Clock3, Database } from "lucide-react";
import type { TurnUsage } from "../lib/types";
import { getLocale, useT } from "../lib/i18n";

const PANEL_GAP = 8;
const PANEL_MARGIN = 12;
const MEASURE_STYLE: CSSProperties = { visibility: "hidden", left: 0, top: 0 };

function samePosition(a: CSSProperties | null, b: CSSProperties): boolean {
  return a?.left === b.left && a?.top === b.top && a?.visibility === b.visibility;
}

function useStatDialog() {
  const [open, setOpen] = useState(false);
  const [position, setPosition] = useState<CSSProperties | null>(null);
  const rootRef = useRef<HTMLSpanElement>(null);
  const panelRef = useRef<HTMLDivElement>(null);

  const updatePosition = useCallback(() => {
    const anchor = rootRef.current;
    const panel = panelRef.current;
    if (!anchor || !panel) return;
    const trigger = anchor.getBoundingClientRect();
    const bounds = panel.getBoundingClientRect();
    const maxLeft = Math.max(PANEL_MARGIN, window.innerWidth - bounds.width - PANEL_MARGIN);
    const left = Math.min(Math.max(trigger.left, PANEL_MARGIN), maxLeft);
    const above = trigger.top - bounds.height - PANEL_GAP;
    const top = above >= PANEL_MARGIN
      ? above
      : Math.min(trigger.bottom + PANEL_GAP, Math.max(PANEL_MARGIN, window.innerHeight - bounds.height - PANEL_MARGIN));
    const next = { left, top } satisfies CSSProperties;
    setPosition(current => samePosition(current, next) ? current : next);
  }, []);

  useLayoutEffect(() => {
    if (!open) {
      setPosition(null);
      return;
    }
    updatePosition();
  }, [open, updatePosition]);

  useEffect(() => {
    if (!open) return;
    const dismiss = (event: PointerEvent) => {
      const target = event.target as Node;
      if (!rootRef.current?.contains(target) && !panelRef.current?.contains(target)) setOpen(false);
    };
    const keydown = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        setOpen(false);
        rootRef.current?.querySelector<HTMLButtonElement>("button")?.focus();
      }
    };
    document.addEventListener("pointerdown", dismiss, true);
    document.addEventListener("keydown", keydown);
    window.addEventListener("resize", updatePosition);
    window.addEventListener("scroll", updatePosition, true);
    return () => {
      document.removeEventListener("pointerdown", dismiss, true);
      document.removeEventListener("keydown", keydown);
      window.removeEventListener("resize", updatePosition);
      window.removeEventListener("scroll", updatePosition, true);
    };
  }, [open, updatePosition]);

  return { open, setOpen, rootRef, panelRef, position };
}

function localeTag(): string {
  return getLocale() === "zh" ? "zh-CN" : getLocale();
}

function exactTokens(value: number): string {
  return `${new Intl.NumberFormat(localeTag()).format(Math.max(0, value))} tok`;
}

function compactTokens(value: number): string {
  const scaled = (candidate: number) => candidate >= 100
    ? String(Math.round(candidate))
    : String(Math.round(candidate * 10) / 10);
  if (value < 1_000) return String(value);
  if (value < 1_000_000) return `${scaled(value / 1_000)}K`;
  return `${scaled(value / 1_000_000)}M`;
}

function formatRunDuration(durationMs: number): string {
  const total = Math.max(0, Math.floor(durationMs / 1000));
  const hours = Math.floor(total / 3600);
  const minutes = Math.floor(total / 60) % 60;
  const seconds = total % 60;
  const zh = getLocale() !== "en";
  if (hours > 0) return zh
    ? `${hours}时 ${String(minutes).padStart(2, "0")}分 ${String(seconds).padStart(2, "0")}秒`
    : `${hours}h ${String(minutes).padStart(2, "0")}m ${String(seconds).padStart(2, "0")}s`;
  if (minutes > 0) return zh
    ? `${minutes}分 ${String(seconds).padStart(2, "0")}秒`
    : `${minutes}m ${String(seconds).padStart(2, "0")}s`;
  return zh ? `${seconds}秒` : `${seconds}s`;
}

export function formatMessageClock(time: number, now = Date.now()): string {
  const date = new Date(time);
  const current = new Date(now);
  const clock = `${String(date.getHours()).padStart(2, "0")}:${String(date.getMinutes()).padStart(2, "0")}`;
  const sameDay = date.getFullYear() === current.getFullYear()
    && date.getMonth() === current.getMonth()
    && date.getDate() === current.getDate();
  if (sameDay) return clock;
  const day = date.getFullYear() === current.getFullYear()
    ? `${date.getMonth() + 1}/${date.getDate()}`
    : `${date.getFullYear()}/${date.getMonth() + 1}/${date.getDate()}`;
  return `${day} ${clock}`;
}

export function TurnUsagePanel({ usage }: { usage: TurnUsage }) {
  const t = useT();
  const dialog = useStatDialog();
  const routes = usage.routes?.join(", ") ?? "";
  const inputTotal = Math.max(0, usage.totalTokens - usage.outputTokens);
  const cacheHit = usage.cacheReadTokens == null || inputTotal <= 0
    ? undefined
    : `${((usage.cacheReadTokens / inputTotal) * 100).toFixed(1)}%`;
  return <span ref={dialog.rootRef} className="chat-stat-root">
    <button type="button" className="chat-stat-trigger" aria-haspopup="dialog" aria-expanded={dialog.open}
      onClick={() => dialog.setOpen(!dialog.open)}>
      <Database aria-hidden="true" /><span>{t("chat.turnUsage", { total: `${compactTokens(usage.totalTokens)} tok` })}</span>
    </button>
    {dialog.open && createPortal(<div ref={dialog.panelRef} className="chat-stat-dialog" role="dialog"
      aria-label={t("chat.turnUsageTitle")} style={dialog.position ?? MEASURE_STYLE}>
      <div className="chat-stat-dialog__title"><strong><Database aria-hidden="true" />{t("chat.turnUsageTitle")}</strong><b>{exactTokens(usage.totalTokens)}</b></div>
      <div className="chat-stat-dialog__rule" aria-hidden="true" />
      <dl className="chat-stat-dialog__details" data-turn-usage-details>
        {routes ? <><dt>{t("chat.turnUsageModel")}</dt><dd className="chat-stat-dialog__route" title={routes}>{routes}</dd></> : null}
        {cacheHit ? <><dt>{t("chat.turnUsageCacheHit")}</dt><dd>{cacheHit}</dd></> : null}
        <dt>{t("chat.turnUsageInput")}</dt><dd>{exactTokens(usage.uncachedInputTokens)}</dd>
        {usage.cacheReadTokens != null ? <><dt>{t("chat.turnUsageCacheRead")}</dt><dd>{exactTokens(usage.cacheReadTokens)}</dd></> : null}
        <dt>{t("chat.turnUsageOutput")}</dt><dd>{exactTokens(usage.outputTokens)}{(usage.reasoningTokens ?? 0) > 0
          ? <span className="chat-stat-dialog__reasoning">{t("chat.turnUsageReasoning", { tokens: exactTokens(usage.reasoningTokens!) })}</span>
          : null}</dd>
      </dl>
    </div>, document.body)}
  </span>;
}

export function TurnTimePanel({ durationMs, tokensPerSecond }: { durationMs: number; tokensPerSecond?: number }) {
  const t = useT();
  const dialog = useStatDialog();
  const duration = formatRunDuration(durationMs);
  return <span ref={dialog.rootRef} className="chat-stat-root">
    <button type="button" className="chat-stat-trigger" aria-haspopup="dialog" aria-expanded={dialog.open}
      onClick={() => dialog.setOpen(!dialog.open)}>
      <Clock3 aria-hidden="true" /><span>{t("chat.turnTime", { duration })}</span>
    </button>
    {dialog.open && createPortal(<div ref={dialog.panelRef} className="chat-stat-dialog" role="dialog"
      aria-label={t("chat.turnTimeTitle")} style={dialog.position ?? MEASURE_STYLE}>
      <div className="chat-stat-dialog__title"><strong><Clock3 aria-hidden="true" />{t("chat.turnTimeTitle")}</strong></div>
      <div className="chat-stat-dialog__rule" aria-hidden="true" />
      <dl className="chat-stat-dialog__details" data-turn-time-details>
        <dt>{t("chat.turnTimeDuration")}</dt><dd>{duration}</dd>
        {tokensPerSecond != null && tokensPerSecond > 0 ? <><dt>{t("chat.turnTimeSpeed")}</dt><dd>{t("chat.tokensPerSecond", { tps: tokensPerSecond >= 10 ? Math.round(tokensPerSecond) : Math.round(tokensPerSecond * 10) / 10 })}</dd></> : null}
      </dl>
    </div>, document.body)}
  </span>;
}
