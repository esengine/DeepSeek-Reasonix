import { Box, type DOMElement, Text, useBoxMetrics } from "ink";
// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React, { useEffect, useRef } from "react";
import { CardRenderer } from "../cards/CardRenderer.js";
import type { Card } from "../state/cards.js";
import { useAgentState } from "../state/provider.js";
import { FG } from "../theme/tokens.js";

/**
 * Row-precision virtual scroll: outer Box clips with overflow="hidden",
 * inner Box holds all cards and slides up via negative marginTop. Yoga
 * computes inner height from card layout; useBoxMetrics reports it back
 * so App can clamp scroll bounds. New cards arriving while the user is
 * scrolled up sit below the visible window — bytes outside outer bounds
 * aren't written, so terminal text selection survives streaming activity.
 */
export function CardStream({
  scrollRows,
  onMaxScrollChange,
  suppressLive = false,
}: {
  scrollRows: number;
  onMaxScrollChange: (rows: number) => void;
  suppressLive?: boolean;
}): React.ReactElement {
  const cards = useAgentState((s) => s.cards);
  // useBoxMetrics types its ref as RefObject<DOMElement> (non-null); the
  // null! assertion satisfies the signature — Ink populates the ref on
  // mount before the metrics hook reads it.
  const outerRef = useRef<DOMElement>(null!);
  const innerRef = useRef<DOMElement>(null!);
  const outer = useBoxMetrics(outerRef);
  const inner = useBoxMetrics(innerRef);
  const maxScroll = Math.max(0, inner.height - outer.height);

  useEffect(() => {
    onMaxScrollChange(maxScroll);
  }, [maxScroll, onMaxScrollChange]);

  let visible = cards;
  if (suppressLive && cards.length > 0 && !isFullySettled(cards[cards.length - 1]!)) {
    visible = cards.slice(0, -1);
  }

  return (
    <>
      {scrollRows > 0 ? <Text color={FG.faint}>{" ↑ earlier — PgUp / wheel / ↑"}</Text> : null}
      <Box ref={outerRef} flexDirection="column" flexGrow={1} overflow="hidden">
        <Box ref={innerRef} flexDirection="column" marginTop={-scrollRows} flexShrink={0}>
          {visible.map((card) => (
            <CardRenderer key={card.id} card={card} />
          ))}
        </Box>
      </Box>
    </>
  );
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
