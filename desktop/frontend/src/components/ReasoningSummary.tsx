import { memo, useEffect, useMemo, useRef } from "react";
import { reasoningSummaryText } from "../lib/reasoningSummary";
import { useReasoningDisplayMode } from "../lib/reasoningDisplayPreference";

export const ReasoningSummary = memo(function ReasoningSummary({
  text,
  streaming = false,
  className = "",
  onOpen,
  maxChars,
}: {
  text: string;
  streaming?: boolean;
  className?: string;
  onOpen?: () => void;
  maxChars?: number;
}) {
  const displayMode = useReasoningDisplayMode();
  const enabled = displayMode === "summary" || displayMode === "auto";
  const summary = useMemo(() => enabled ? reasoningSummaryText(text, { streaming, maxChars }) : "", [enabled, text, streaming, maxChars]);
  const ref = useRef<HTMLElement>(null);

  // While streaming, keep the single-line summary pinned to the line tail so
  // the newest text stays visible; rAF coalesces rapid token updates. A
  // settled summary resets to the line start.
  useEffect(() => {
    if (!enabled) return;
    const el = ref.current;
    if (!el) return;
    if (!streaming) {
      el.scrollLeft = 0;
      return;
    }
    const frame = requestAnimationFrame(() => {
      el.scrollLeft = el.scrollWidth;
    });
    return () => cancelAnimationFrame(frame);
  }, [enabled, summary, streaming]);

  if (!enabled || !summary) return null;
  const cls = `reasoning-summary${className ? ` ${className}` : ""}`;
  const followEnd = streaming ? { "data-follow-end": "" } : {};
  if (onOpen) {
    return <button ref={ref as React.RefObject<HTMLButtonElement>} type="button" className={cls} onClick={onOpen} {...followEnd}>{summary}</button>;
  }
  return <span ref={ref as React.RefObject<HTMLSpanElement>} className={cls} {...followEnd}>{summary}</span>;
});
