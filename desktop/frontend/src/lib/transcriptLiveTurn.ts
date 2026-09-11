import type { AssistantItem } from "./transcriptRows";

/** The answer presentation never duplicates reasoning that already belongs to
 * the turn's work-process segment. */
export function assistantAnswerOnly(item: AssistantItem): AssistantItem {
  return { ...item, reasoning: "", reasoningComplete: true, reasoningDurationMs: undefined };
}
