import { Box, Static } from "ink";
import React, { useMemo } from "react";
import { CardRenderer } from "../cards/CardRenderer.js";
import type { Card } from "../state/cards.js";
import { useAgentState } from "../state/provider.js";
import { isFullySettled } from "./CardStream.js";

export function StaticCardStream({
  suppressLive = false,
}: {
  suppressLive?: boolean;
}): React.ReactElement {
  const cards = useAgentState((s) => s.cards);
  const { settled, live } = useMemo(() => partition(cards), [cards]);
  const visibleLive = suppressLive && live.length > 0 ? live.slice(0, -1) : live;
  return (
    <>
      <Static items={settled}>
        {(card) => (
          <Box key={card.id} flexDirection="column" flexShrink={0}>
            <CardRenderer card={card} />
          </Box>
        )}
      </Static>
      <Box flexDirection="column" flexShrink={0}>
        {visibleLive.map((card) => (
          <Box key={card.id} flexDirection="column" flexShrink={0}>
            <CardRenderer card={card} />
          </Box>
        ))}
      </Box>
    </>
  );
}

function partition(cards: readonly Card[]): { settled: Card[]; live: Card[] } {
  const firstUnsettled = cards.findIndex((c) => !isFullySettled(c));
  if (firstUnsettled === -1) return { settled: [...cards], live: [] };
  return { settled: cards.slice(0, firstUnsettled), live: cards.slice(firstUnsettled) };
}
