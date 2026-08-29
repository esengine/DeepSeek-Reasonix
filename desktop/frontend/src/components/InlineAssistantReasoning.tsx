import { useCallback, useContext, useEffect, useRef, useState } from "react";
import { ChevronRight } from "lucide-react";
import { useReasoningDisplayMode } from "../lib/reasoningDisplayPreference";
import type { AssistantItem } from "../lib/transcriptRows";
import { useT } from "../lib/i18n";
import { LiveStreamContext } from "./LiveStreamContext";
import { Markdown } from "./Markdown";
import { ProcessBrainIcon } from "./ProcessCard";
import { ReasoningSummary } from "./ReasoningSummary";
import { StreamingReasoningText } from "./StreamingReasoningText";
import { useTranscriptUserResizeIntent } from "./TranscriptLayoutIntentContext";
import { resolveReasoningLayoutVariant } from "../lib/transcriptRowGeometry";
import { useReasoningScrollFollow } from "../lib/useReasoningScrollFollow";

export function InlineAssistantReasoning({
  item,
  autoFollowActive,
  onManualOpen,
}: {
  item: AssistantItem;
  autoFollowActive?: boolean;
  onManualOpen?: () => void;
}) {
  const t = useT();
  const beginUserResize = useTranscriptUserResizeIntent();
  const live = useContext(LiveStreamContext);
  const displayMode = useReasoningDisplayMode();
  const shown = live?.id === item.id ? { reasoning: live.reasoning, streaming: true, reasoningComplete: live.reasoningComplete } : item;
  const running = shown.streaming && !shown.reasoningComplete;
  const followActive = autoFollowActive ?? shown.streaming;
  const [open, setOpen] = useState(displayMode === "expanded" || (displayMode === "auto" && followActive));
  const userOverridden = useRef(false);
  const previousRunning = useRef(running);
  const previousFollowActive = useRef(followActive);
  const previousMode = useRef(displayMode);
  useEffect(() => {
    const modeChanged = previousMode.current !== displayMode;
    const wasRunning = previousRunning.current;
    const wasFollowActive = previousFollowActive.current;
    previousMode.current = displayMode;
    previousRunning.current = running;
    previousFollowActive.current = followActive;
    if (modeChanged) {
      userOverridden.current = false;
      setOpen(displayMode === "expanded" || (displayMode === "auto" && followActive));
    } else if (running && !wasRunning && (displayMode === "auto" || displayMode === "expanded")) {
      userOverridden.current = false;
      setOpen(true);
    } else if (displayMode === "auto" && !followActive && wasFollowActive && !userOverridden.current) {
      setOpen(false);
    }
  }, [displayMode, followActive, running]);
  const toggle = useCallback(() => {
    beginUserResize();
    userOverridden.current = true;
    if (!open) onManualOpen?.();
    setOpen(!open);
  }, [beginUserResize, onManualOpen, open]);
  const reasoning = shown.reasoning.trim();
  const [reasoningScrollRef, onReasoningScroll] = useReasoningScrollFollow(shown.reasoning, open && running);
  if (!reasoning) return null;
  const layoutVariant = open
    ? "reasoning-expanded"
    : resolveReasoningLayoutVariant(displayMode, followActive) ?? "reasoning-heading-only";
  return (
    <div
      className={`turn-collapse__reasoning-phase${open ? " turn-collapse__reasoning-phase--open" : ""}`}
      data-transcript-layout-variant={layoutVariant}
    >
      <button type="button" className="turn-collapse__reasoning-head" data-running={running ? "" : undefined} onClick={toggle} aria-expanded={open}>
        <ProcessBrainIcon size={12} />
        <span>{running ? t("msg.thinkingRunning") : t("msg.thinking")}</span>
        <ChevronRight className={`reasoning__chevron${open ? " reasoning__chevron--open" : ""}`} size={12} />
      </button>
      {open ? (
        <div
          ref={reasoningScrollRef}
          className="turn-collapse__inline-reasoning reasoning__body"
          data-transcript-selectable="reasoning"
          data-nested-scroll
          onScroll={onReasoningScroll}
        >
          {running
            ? <StreamingReasoningText text={shown.reasoning} />
            : <Markdown text={shown.reasoning} streaming={false} cacheKey={item.id} wasStreamed={item.wasStreamed} />}
        </div>
      ) : (
        <ReasoningSummary text={shown.reasoning} streaming={running} onOpen={toggle} />
      )}
    </div>
  );
}
