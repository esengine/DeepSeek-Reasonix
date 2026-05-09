import type { Card } from "../state/cards.js";
import { useAgentState } from "../state/provider.js";

export function deriveActivityLabel(cards: ReadonlyArray<Card>): string {
  if (cards.some((c) => c.kind === "reasoning" && c.streaming)) return "thinking…";
  const last = cards[cards.length - 1];
  if (!last || last.kind === "user") return "waiting for model…";
  return "processing…";
}

export function useActivityLabel(): string {
  return useAgentState((s) => deriveActivityLabel(s.cards));
}
