import { Static } from "ink";
// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React from "react";
import { CardRenderer } from "../cards/CardRenderer.js";
import { ActiveCardContext } from "../primitives/Card.js";
import type { Card } from "../state/cards.js";
import { useAgentState } from "../state/provider.js";

/** Settled = no future event can mutate it; safe to commit via Ink's Static. */
function isSettled(card: Card): boolean {
  switch (card.kind) {
    case "streaming":
    case "tool":
    case "branch":
      return card.done || !!card.aborted;
    case "reasoning":
      return !card.streaming || !!card.aborted;
    default:
      return true;
  }
}

export function splitCardStream(
  cards: readonly Card[],
  suppressLive = false,
): { committed: Card[]; live: Card[] } {
  // Static appends in order and never re-renders prior items, so a card
  // can only commit once it's settled AND every earlier card is too —
  // otherwise reasoning(streaming=true) freezes mid-stream when a later
  // streaming/tool card appears (issue: spinner survives reasoning.end).
  const firstUnsettledIdx = cards.findIndex((c) => !isSettled(c));
  if (firstUnsettledIdx === -1) {
    return { committed: cards.slice(), live: [] };
  }
  const committed: Card[] = cards.slice(0, firstUnsettledIdx);
  const live: Card[] = cards.slice(firstUnsettledIdx);
  if (suppressLive && live.length > 0 && !isSettled(live[live.length - 1]!)) {
    return { committed, live: live.slice(0, -1) };
  }
  return { committed, live };
}

export function CardStream({
  suppressLive = false,
}: {
  suppressLive?: boolean;
}): React.ReactElement {
  const cards = useAgentState((s) => s.cards);
  const { committed, live } = splitCardStream(cards, suppressLive);
  // Static items are emitted via bridge.emitStatic, which renders them in an
  // off-tree React reconciler — context from the live tree does NOT propagate.
  // The ActiveCardContext.Provider must therefore live inside the children
  // function so it travels with the rendered subtree.
  return (
    <>
      <Static items={committed}>
        {(card) => (
          <ActiveCardContext.Provider value={false} key={card.id}>
            <CardRenderer card={card} />
          </ActiveCardContext.Provider>
        )}
      </Static>
      {live.map((card) => (
        <CardRenderer key={card.id} card={card} />
      ))}
    </>
  );
}
