// ProcessFoldHeader: the fold header row of one process segment.
// The fold body is NOT rendered here: an open fold contributes its body rows
// to the virtual row model (they mount only when scrolled into view), a closed
// fold builds no React subtree at all.

import { useContext } from "react";
import { ChevronRight } from "lucide-react";
import { useT } from "../lib/i18n";
import type { SegmentModel } from "../lib/transcriptRows";
import { useTick, workStatusLabel } from "../lib/workStatus";
import { LiveStreamContext } from "./LiveStreamContext";

export function ProcessFoldHeader({
  segment,
  open,
  onToggle,
  turnStartAt,
}: {
  segment: SegmentModel;
  open: boolean;
  onToggle: () => void;
  turnStartAt?: number;
}) {
  const t = useT();
  const live = useContext(LiveStreamContext);
  const displayItems = segment.displayItems;

  const hasRunningWork = segment.hasRunningWork;
  const now = useTick(hasRunningWork);
  const runningDurationMs = hasRunningWork
    ? turnStartAt
      ? Math.max(0, now - turnStartAt)
      : live?.reasoningStartedAt
        ? Math.max(0, now - live.reasoningStartedAt)
        : 0
    : 0;
  const effectiveDurationMs = hasRunningWork ? Math.max(segment.durationMs, runningDurationMs) : segment.durationMs;

  const baseLabel = workStatusLabel(effectiveDurationMs, hasRunningWork, t);
  // Surface what the closed fold hides — a bare duration reads as pure timing
  // and users have no way to know process detail sits behind it.
  const toolCount = displayItems.reduce((n, it) => n + (it.kind === "tool" ? 1 : 0), 0);
  const thoughtCount = displayItems.reduce((n, it) => n + (it.kind === "assistant" ? 1 : 0), 0);
  const countParts: string[] = [];
  if (toolCount > 0) countParts.push(t("transcript.toolCount", { n: toolCount }));
  if (thoughtCount > 0) countParts.push(t("transcript.thoughtCount", { n: thoughtCount }));
  const label = segment.labelStyle === "counts"
    ? (countParts.length > 0 ? countParts.join(" · ") : t("transcript.processed"))
    : countParts.length > 0
      ? `${baseLabel} · ${countParts.join(" · ")}`
      : baseLabel;
  return (
    <div className={`turn-collapse${open ? " turn-collapse--open" : ""}`} data-kind="reasoning" data-entrance={displayItems[0]?.id || undefined}>
      <button
        type="button"
        className="reasoning__head"
        onClick={onToggle}
        aria-expanded={open}
      >
        <span className="turn-collapse__label" data-creation-label={label}>{label}</span>
        {!hasRunningWork && <ChevronRight className={`reasoning__chevron${open ? " reasoning__chevron--open" : ""}`} size={12} />}
      </button>
    </div>
  );
}
