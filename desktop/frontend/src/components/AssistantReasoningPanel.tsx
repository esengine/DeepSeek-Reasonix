import { useEffect, useRef, useState } from "react";
import { ChevronRight } from "lucide-react";
import { displayReasoningText, STREAMING_REASONING_WINDOW_STEP_CHARS, STREAMING_REASONING_WINDOW_STEP_LINES } from "../lib/reasoningDisplay";
import { useReasoningDisplayMode } from "../lib/reasoningDisplayPreference";
import { useCollapseAnimation } from "../lib/useCollapseAnimation";
import { useT } from "../lib/i18n";
import type { Item } from "../lib/useController";
import { Markdown } from "./Markdown";
import { ProcessBrainIcon } from "./ProcessCard";
import { ReasoningSummary } from "./ReasoningSummary";

type AssistantItem = Extract<Item, { kind: "assistant" }>;

function reasoningDurationLabel(durationMs: number | undefined, t: ReturnType<typeof useT>): string {
  if (typeof durationMs !== "number" || !Number.isFinite(durationMs) || durationMs <= 0) return t("msg.thinkingDone");
  return t("msg.thinkingDuration", { s: Math.max(1, Math.round(durationMs / 1000)) });
}

export function AssistantReasoningPanel({
  item,
  defaultExpanded,
  expandWhileStreaming,
  truncateStreamingReasoning,
}: {
  item: AssistantItem;
  defaultExpanded: boolean;
  expandWhileStreaming: boolean;
  truncateStreamingReasoning: boolean;
}) {
  const t = useT();
  const displayMode = useReasoningDisplayMode();
  const running = item.streaming && !item.reasoningComplete;
  const followsWhileStreaming = displayMode === "auto" || expandWhileStreaming;
  const [open, setOpen] = useState(defaultExpanded || (followsWhileStreaming && running));
  const bodyRef = useRef<HTMLDivElement>(null);
  const userOverridden = useRef(false);
  const previousStreaming = useRef(item.streaming);
  const previousComplete = useRef(item.reasoningComplete ?? false);
  const previousMode = useRef(displayMode);

  useEffect(() => {
    const wasStreaming = previousStreaming.current;
    const wasComplete = previousComplete.current;
    const complete = item.reasoningComplete ?? false;
    const modeChanged = previousMode.current !== displayMode;
    previousStreaming.current = item.streaming;
    previousComplete.current = complete;
    previousMode.current = displayMode;
    if (modeChanged) {
      userOverridden.current = false;
      setOpen(defaultExpanded || (followsWhileStreaming && !complete));
    } else if (item.streaming) {
      if (!wasStreaming) userOverridden.current = false;
      if (defaultExpanded) setOpen(true);
      else if (!userOverridden.current) setOpen(followsWhileStreaming && !complete);
    } else if ((complete && !wasComplete) || wasStreaming) {
      if (!defaultExpanded && !userOverridden.current) setOpen(false);
    }
  }, [defaultExpanded, displayMode, followsWhileStreaming, item.reasoningComplete, item.streaming]);

  const toggle = () => {
    userOverridden.current = true;
    setOpen((value) => !value);
  };
  useCollapseAnimation(bodyRef, open);
  if (displayMode === "hidden" || displayMode === "pending") return null;
  const visibleReasoning = open ? displayReasoningText(item.reasoning, {
    streaming: running,
    truncateStreaming: truncateStreamingReasoning,
    stableWindowChars: STREAMING_REASONING_WINDOW_STEP_CHARS,
    stableWindowLines: STREAMING_REASONING_WINDOW_STEP_LINES,
  }) : "";
  const meta = running ? "" : reasoningDurationLabel(item.reasoningDurationMs, t);
  return (
    <div className="reasoning">
      <button type="button" className="reasoning__head" data-running={running ? "" : undefined} onClick={toggle} aria-expanded={open}>
        <ProcessBrainIcon size={12} />
        <span data-creation-label={t("creation.reasoningLabel")}>{running ? t("msg.thinkingRunning") : t("msg.thinking")}</span>
        {meta && <span className="reasoning__meta">{meta}</span>}
        <ChevronRight className={`reasoning__chevron${open ? " reasoning__chevron--open" : ""}`} size={12} />
      </button>
      {open ? <div ref={bodyRef} className="reasoning__body"><Markdown text={visibleReasoning} streaming={running} /></div>
        : <ReasoningSummary text={item.reasoning} streaming={running} onOpen={toggle} />}
    </div>
  );
}
