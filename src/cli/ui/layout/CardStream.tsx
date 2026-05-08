import { Box, type DOMElement, Text, measureElement } from "ink";
// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React, { useEffect, useRef, useState } from "react";
import { CardRenderer } from "../cards/CardRenderer.js";
import type { Card } from "../state/cards.js";
import { useAgentState } from "../state/provider.js";
import { FG } from "../theme/tokens.js";

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
  const outerRef = useRef<DOMElement>(null!);
  const innerRef = useRef<DOMElement>(null!);
  const [outerHeight, setOuterHeight] = useState(0);
  const [innerHeight, setInnerHeight] = useState(0);
  const maxScroll = Math.max(0, innerHeight - outerHeight);

  useEffect(() => {
    setOuterHeight(measureElement(outerRef.current).height);
    setInnerHeight(measureElement(innerRef.current).height);
  });

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
    case "branch":
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
