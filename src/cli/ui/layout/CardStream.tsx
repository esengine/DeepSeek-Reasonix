import { Box, type DOMElement, Text, useBoxMetrics } from "ink";
// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React, { useEffect, useRef } from "react";
import { CardRenderer } from "../cards/CardRenderer.js";
import type { Card } from "../state/cards.js";
import { useChatScrollActions, useChatScrollState } from "../state/chat-scroll-provider.js";
import { useAgentState } from "../state/provider.js";
import { FG, TONE } from "../theme/tokens.js";

/**
 * Row-precision virtual scroll: outer Box clips with overflow="hidden",
 * inner Box holds all cards and slides up via negative marginTop.
 *
 * Reads scrollRows from the chat-scroll store directly so wheel/arrow
 * ticks don't go through App.tsx's render. setMaxScroll is reported
 * back once Yoga has measured both Box refs.
 */
export function CardStream({
  suppressLive = false,
}: {
  suppressLive?: boolean;
}): React.ReactElement {
  const cards = useAgentState((s) => s.cards);
  const scrollRows = useChatScrollState((s) => s.scrollRows);
  const { setMaxScroll } = useChatScrollActions();
  const outerRef = useRef<DOMElement>(null!);
  const innerRef = useRef<DOMElement>(null!);
  const outer = useBoxMetrics(outerRef);
  const inner = useBoxMetrics(innerRef);
  const maxScroll = Math.max(0, inner.height - outer.height);

  useEffect(() => {
    setMaxScroll(maxScroll);
  }, [maxScroll, setMaxScroll]);

  let visible = cards;
  if (suppressLive && cards.length > 0 && !isFullySettled(cards[cards.length - 1]!)) {
    visible = cards.slice(0, -1);
  }

  return (
    <>
      {/* Always reserve the row — making it conditional ties outer.height to scrollRows and closes a setState loop with pinned mode. */}
      <Box height={1} flexShrink={0}>
        {scrollRows > 0 ? <ScrollIndicator scrollRows={scrollRows} maxScroll={maxScroll} /> : null}
      </Box>
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

/** Position indicator in the row above the viewport. Briefly highlights on every
 * scroll tick (scrollVersion bump) so the user gets visual confirmation that
 * the wheel/arrow registered, even before the new frame paints. */
function ScrollIndicator({
  scrollRows,
  maxScroll,
}: { scrollRows: number; maxScroll: number }): React.ReactElement {
  const version = useChatScrollState((s) => s.scrollVersion);
  const [hot, setHot] = React.useState(false);
  React.useEffect(() => {
    if (version === 0) return;
    setHot(true);
    const id = setTimeout(() => setHot(false), 220);
    return () => clearTimeout(id);
  }, [version]);
  const remaining = Math.max(0, maxScroll - scrollRows);
  const text = ` ↑ ${scrollRows} / ${maxScroll} rows above${remaining > 0 ? ` — ${remaining} more` : ""} · PgUp / wheel / ↑`;
  return <Text color={hot ? TONE.accent : FG.faint}>{text}</Text>;
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
