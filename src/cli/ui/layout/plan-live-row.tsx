// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React from "react";
import { PlanCard } from "../cards/PlanCard.js";
import type { Card, PlanCard as PlanCardData } from "../state/cards.js";
import { useAgentState } from "../state/provider.js";

export function isActivePlanInFlight(card: Card): boolean {
  if (card.kind !== "plan") return false;
  if (card.variant !== "active") return false;
  return !card.steps.every((s) => s.status === "done" || s.status === "skipped");
}

export function PlanLiveRow(): React.ReactElement | null {
  const planCard = useAgentState((s) => {
    for (let i = s.cards.length - 1; i >= 0; i--) {
      const c = s.cards[i] as Card;
      if (isActivePlanInFlight(c)) return c as PlanCardData;
    }
    return null;
  });
  if (!planCard) return null;
  return <PlanCard card={planCard} />;
}
