import { Box, Static } from "ink";
import React, { useEffect, useMemo, useRef, useState } from "react";
import { CardRenderer } from "../cards/CardRenderer.js";
import { useRenderTrace } from "../render-trace.js";
import type { Card } from "../state/cards.js";
import { useAgentState } from "../state/provider.js";

interface StaticCardStreamProps {
  suppressLive?: boolean;
}

/** First chunk committed synchronously on mount — sized for ~1 frame of paint cost. */
const INITIAL_BATCH = 30;
/** Each subsequent batch released after a yield to the event loop. */
const PROGRESSIVE_BATCH = 30;

function StaticCardStreamInner({
  suppressLive = false,
}: StaticCardStreamProps): React.ReactElement {
  useRenderTrace("StaticCardStream");
  const cards = useAgentState((s) => s.cards);
  const visibleCards = useProgressiveBacklog(cards);
  const { staticItems, dynamicItems, hasUnsettledDynamic } = useMemo(
    () => partition(visibleCards),
    [visibleCards],
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

/** Snapshot the initial backlog on first non-empty render; drain it in batches via
 *  setImmediate so first paint shows ~INITIAL_BATCH cards immediately and the
 *  event loop stays responsive for input. New cards added after drain bypass
 *  the gate. New cards added DURING drain are held back until the backlog
 *  catches up — keeps Static's append-only contract intact so chronological
 *  order is preserved. Gates the FULL cards array (not just the static partition)
 *  so verbose-sensitive tool/reasoning cards in the live region also drip. */
function useProgressiveBacklog(cards: readonly Card[]): Card[] {
  const backlogRef = useRef<number | null>(null);
  if (backlogRef.current === null && cards.length > 0) {
    backlogRef.current = cards.length;
  }
  const backlog = backlogRef.current ?? 0;
  const [released, setReleased] = useState(() => Math.min(INITIAL_BATCH, backlog));

  // Catch the case where we mounted empty and the backlog snapshot happens on a
  // later render — re-seed `released` so it tracks the freshly-snapshotted total.
  if (backlog > 0 && released === 0) {
    setReleased(Math.min(INITIAL_BATCH, backlog));
  }

  const draining = released < backlog;
  // biome-ignore lint/correctness/useExhaustiveDependencies: `released` IS the cursor — each batch's state update must re-fire the effect to schedule the next batch. Removing it from the deps would deliver only one batch and stall the drain.
  useEffect(() => {
    if (!draining) return;
    const handle = setImmediate(() => {
      setReleased((r) => Math.min(backlog, r + PROGRESSIVE_BATCH));
    });
    return () => clearImmediate(handle);
  }, [draining, released, backlog]);

  if (!draining) return cards.slice();
  // Drop the held-back middle. Always include cards added AFTER the snapshot
  // (indices >= backlog) so new live activity isn't blocked by an old backlog.
  return cards.slice(0, released).concat(cards.slice(backlog));
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
