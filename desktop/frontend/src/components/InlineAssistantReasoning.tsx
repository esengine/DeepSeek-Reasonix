import { useCallback, useContext, useEffect, useRef, useState } from "react";
import { ChevronRight } from "lucide-react";
import { useWorkProcessPresentation } from "../lib/sessionExperience";
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
  const presentation = useWorkProcessPresentation();
  const shown = live?.id === item.id ? { reasoning: live.reasoning, streaming: true, reasoningComplete: live.reasoningComplete } : item;
  const running = shown.streaming && !shown.reasoningComplete;
  const followActive = autoFollowActive ?? shown.streaming;
  const [open, setOpen] = useState(presentation.keepExpandedAfterCompletion || (presentation.showWhileRunning && followActive));
  const userOverridden = useRef(false);
  const previousRunning = useRef(running);
  const previousFollowActive = useRef(followActive);
  const previousExperience = useRef(presentation.experience);
  useEffect(() => {
    const modeChanged = previousExperience.current !== presentation.experience;
    const wasRunning = previousRunning.current;
    const wasFollowActive = previousFollowActive.current;
    previousExperience.current = presentation.experience;
    previousRunning.current = running;
    previousFollowActive.current = followActive;
    if (modeChanged) {
      userOverridden.current = false;
      setOpen(presentation.keepExpandedAfterCompletion || (presentation.showWhileRunning && followActive));
    } else if (running && !wasRunning && presentation.showWhileRunning) {
      userOverridden.current = false;
      setOpen(true);
    } else if (!presentation.keepExpandedAfterCompletion && !followActive && wasFollowActive && !userOverridden.current) {
      setOpen(false);
    }
  }, [followActive, presentation, running]);
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
    : resolveReasoningLayoutVariant(presentation.keepExpandedAfterCompletion ? "expanded" : "summary", followActive) ?? "reasoning-heading-only";
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
