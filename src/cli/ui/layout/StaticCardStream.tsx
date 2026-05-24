import { Box, Static } from "ink";
import React, { useMemo } from "react";
import { CardRenderer } from "../cards/CardRenderer.js";
import type { Card } from "../state/cards.js";
import { useAgentState } from "../state/provider.js";

interface StaticCardStreamProps {
  suppressLive?: boolean;
}

function StaticCardStreamInner({
  suppressLive = false,
}: StaticCardStreamProps): React.ReactElement {
  const cards = useAgentState((s) => s.cards);
  const { staticItems, dynamicItems, hasUnsettledDynamic } = useMemo(
    () => partition(cards),
    [cards],
  );
  const visibleDynamic =
    suppressLive && hasUnsettledDynamic && dynamicItems.length > 0
      ? dynamicItems.slice(0, -1)
      : dynamicItems;
  return (
    <>
      <Static items={staticItems}>
        {(card) => (
          <Box key={card.id} flexDirection="column" flexShrink={0}>
            <CardRenderer card={card} />
          </Box>
        )}
      </Static>
      <Box flexDirection="column" flexShrink={0}>
        {visibleDynamic.map((card) => (
          <Box key={card.id} flexDirection="column" flexShrink={0}>
            <CardRenderer card={card} />
          </Box>
        ))}
      </Box>
    </>
  );
}

export const StaticCardStream = React.memo(StaticCardStreamInner);
StaticCardStream.displayName = "StaticCardStream";

function partition(cards: readonly Card[]): {
  staticItems: Card[];
  dynamicItems: Card[];
  hasUnsettledDynamic: boolean;
} {
  const firstDynamic = cards.findIndex((c) => !isFullySettled(c) || isVerboseSensitive(c));
  if (firstDynamic === -1) {
    return { staticItems: [...cards], dynamicItems: [], hasUnsettledDynamic: false };
  }
  const dynamicItems = cards.slice(firstDynamic);
  return {
    staticItems: cards.slice(0, firstDynamic),
    dynamicItems,
    hasUnsettledDynamic: dynamicItems.some((c) => !isFullySettled(c)),
  };
}

function isVerboseSensitive(card: Card): boolean {
  return card.kind === "reasoning" || card.kind === "tool";
}

function isFullySettled(card: Card): boolean {
  switch (card.kind) {
    case "streaming":
    case "tool":
      return card.done || !!card.aborted;
    case "reasoning":
      return !card.streaming || !!card.aborted;
    case "task":
    case "subagent":
      return card.status !== "running";
    case "plan":
      return card.steps.every((s) => s.status === "done" || s.status === "skipped");
    default:
      return true;
  }
}
