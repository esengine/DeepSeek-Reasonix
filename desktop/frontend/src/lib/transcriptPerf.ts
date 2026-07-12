import type { Item } from "./useController";

export const DEFAULT_HOT_TURNS = 30;
export const DEFAULT_WARM_PAGE_SIZE = 20;

export interface TranscriptLayerBudget {
  hotTurns: number;
  pageSize: number;
}

export function transcriptLayerBudget(items: Item[]): TranscriptLayerBudget {
  let textBytes = 0;
  let toolCount = 0;
  for (const item of items) {
    if (item.kind === "assistant" || item.kind === "user") {
      textBytes += item.text.length;
      if (item.kind === "assistant") textBytes += item.reasoning.length;
    } else if (item.kind === "tool") {
      toolCount += 1;
      textBytes += item.args.length + (item.output?.length ?? 0) + (item.error?.length ?? 0);
    }
  }
  if (items.length > 700 || toolCount > 220 || textBytes > 800_000) return { hotTurns: 10, pageSize: 10 };
  if (items.length > 350 || toolCount > 120 || textBytes > 350_000) return { hotTurns: 16, pageSize: 12 };
  if (items.length > 180 || toolCount > 60 || textBytes > 160_000) return { hotTurns: 22, pageSize: 16 };
  return { hotTurns: DEFAULT_HOT_TURNS, pageSize: DEFAULT_WARM_PAGE_SIZE };
}

