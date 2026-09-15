import type { ResolvedReasoningDisplayMode } from "./reasoningDisplayPreference";
import type { SessionExperience, WorkProcessPresentation } from "./sessionExperience";
import type { Item } from "./useController";
type ToolItem = Extract<Item, { kind: "tool" }>;
export function resolveToolCardDefaultOpen(
  item: ToolItem,
  nestedCount: number,
  reasoningDisplayMode: ResolvedReasoningDisplayMode | SessionExperience | WorkProcessPresentation,
): boolean {
  const experience = typeof reasoningDisplayMode === "object"
    ? reasoningDisplayMode.experience
    : reasoningDisplayMode === "deep" || reasoningDisplayMode === "expanded"
      ? "deep"
      : reasoningDisplayMode === "concise"
        ? "concise"
        : "standard";
  // Concise keeps nested tools and sub-agent chrome collapsed while running.
  const showWhileRunning = typeof reasoningDisplayMode === "object"
    ? reasoningDisplayMode.showWhileRunning
    : experience !== "concise";
  const subagentReasoningRunning = item.subagentProgress?.phase === "reasoning";
  const liveFollow = showWhileRunning;
  // Deep mode exposes the complete tool process by default. Cards without a
  // body remain visually unchanged, while body-bearing cards/groups can still
  // be manually collapsed by the user.
  const keepSubagentReasoningExpanded = experience === "deep";
  return (liveFollow && nestedCount > 0 && item.status === "running")
    || (liveFollow && Boolean(item.subagentProgress) && item.status === "running")
    || (liveFollow && subagentReasoningRunning)
    || keepSubagentReasoningExpanded;
}
