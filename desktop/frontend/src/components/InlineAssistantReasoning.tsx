import { useCallback, useContext, useEffect, useRef, useState } from "react";
import { ChevronRight } from "lucide-react";
import { displayReasoningText, STREAMING_REASONING_WINDOW_STEP_CHARS, STREAMING_REASONING_WINDOW_STEP_LINES } from "../lib/reasoningDisplay";
import { useReasoningDisplayMode } from "../lib/reasoningDisplayPreference";
import type { AssistantItem } from "../lib/transcriptRows";
import { useT } from "../lib/i18n";
import { LiveStreamContext } from "./LiveStreamContext";
import { Markdown } from "./Markdown";
import { ProcessBrainIcon } from "./ProcessCard";
import { ReasoningSummary } from "./ReasoningSummary";

export function InlineAssistantReasoning({ item, onManualOpen }: { item: AssistantItem; onManualOpen?: () => void }) {
  const t = useT();
  const live = useContext(LiveStreamContext);
  const displayMode = useReasoningDisplayMode();
  const shown = live?.id === item.id ? { reasoning: live.reasoning, streaming: true, reasoningComplete: live.reasoningComplete } : item;
  const running = shown.streaming && !shown.reasoningComplete;
  const [open, setOpen] = useState(displayMode === "auto" && running);
  const userOverridden = useRef(false);
  const previousRunning = useRef(running);
  const previousMode = useRef(displayMode);
  useEffect(() => {
    const modeChanged = previousMode.current !== displayMode;
    const wasRunning = previousRunning.current;
    previousMode.current = displayMode;
    previousRunning.current = running;
    if (modeChanged) {
      userOverridden.current = false;
      setOpen(displayMode === "auto" && running);
    } else if (displayMode === "auto" && running && !wasRunning) {
      userOverridden.current = false;
      setOpen(true);
    } else if (displayMode === "auto" && !running && wasRunning && !userOverridden.current) {
      setOpen(false);
    }
  }, [displayMode, running]);
  const toggle = useCallback(() => {
    userOverridden.current = true;
    if (!open) onManualOpen?.();
    setOpen(!open);
  }, [onManualOpen, open]);
  const reasoning = shown.reasoning.trim();
  if (!reasoning) return null;
  const visibleReasoning = open ? displayReasoningText(shown.reasoning, {
    streaming: running,
    truncateStreaming: true,
    stableWindowChars: STREAMING_REASONING_WINDOW_STEP_CHARS,
    stableWindowLines: STREAMING_REASONING_WINDOW_STEP_LINES,
  }) : "";
  return (
    <div className={`turn-collapse__reasoning-phase${open ? " turn-collapse__reasoning-phase--open" : ""}`}>
      <button type="button" className="turn-collapse__reasoning-head" data-running={running ? "" : undefined} onClick={toggle} aria-expanded={open}>
        <ProcessBrainIcon size={12} />
        <span>{running ? t("msg.thinkingRunning") : t("msg.thinking")}</span>
        <ChevronRight className={`reasoning__chevron${open ? " reasoning__chevron--open" : ""}`} size={12} />
      </button>
      {open ? <div className="turn-collapse__inline-reasoning"><Markdown text={visibleReasoning} streaming={running} /></div>
        : <ReasoningSummary text={shown.reasoning} streaming={running} onOpen={toggle} />}
    </div>
  );
}
