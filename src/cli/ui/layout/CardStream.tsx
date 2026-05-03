import { Static } from "ink";
// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React from "react";
import { CardRenderer } from "../cards/CardRenderer.js";
import type { Card } from "../state/cards.js";
import { useAgentState } from "../state/provider.js";

/** Settled = no future event can mutate it; safe to commit via Ink's Static. */
function isSettled(card: Card): boolean {
  switch (card.kind) {
    case "streaming":
    case "tool":
    case "branch":
      return card.done;
    case "reasoning":
      return !card.streaming;
    case "plan":
      if (card.variant !== "active") return true;
      return card.steps.every((s) => s.status === "done" || s.status === "skipped");
    default:
      return true;
  }
}

export function CardStream(): React.ReactElement {
  const cards = useAgentState((s) => s.cards);
  let cutoff = cards.length;
  for (let i = 0; i < cards.length; i++) {
    if (!isSettled(cards[i] as Card)) {
      cutoff = i;
      break;
    }
  }
  const committed = cards.slice(0, cutoff);
  const live = cards.slice(cutoff);
  return (
    <>
      <Static items={committed}>{(card) => <CardRenderer key={card.id} card={card} />}</Static>
      {live.map((card) => (
        <CardRenderer key={card.id} card={card} />
      ))}
    </>
  );
}
