import { Box, type DOMElement, Text, useBoxMetrics } from "ink";
import React, { useEffect, useMemo, useRef } from "react";
import { t } from "../../../i18n/index.js";
import { CardRenderer } from "../cards/CardRenderer.js";
import type { Card } from "../state/cards.js";
import { useChatScrollActions, useChatScrollState } from "../state/chat-scroll-provider.js";
import { useAgentState } from "../state/provider.js";
import { FG, TONE } from "../theme/tokens.js";

/** Buffer of rows kept rendered on each side of the viewport so a single scroll
 * step doesn't reveal an unmeasured card. Larger = smoother but renders more. */
const VISIBLE_BUFFER_ROWS = 30;

/**
 * Row-precision virtual scroll with card-level virtualization.
 *
 * outer Box clips with overflow="hidden"; inner Box holds visible cards
 * plus spacer Boxes for off-screen ranges and slides up via negative
 * marginTop. Off-screen cards are replaced by a single spacer Box of the
 * cumulative height — Yoga skips them entirely on every re-layout.
 *
 * Heights are populated lazily: any card whose height isn't cached yet
 * is rendered live (so it can be measured), then collapses into the
 * spacer once outside the viewport. A streaming card that grows on every
 * delta keeps its height fresh through the same measurement path.
 */
export function CardStream({
  suppressLive = false,
}: {
  suppressLive?: boolean;
}): React.ReactElement {
  const cards = useAgentState((s) => s.cards);
  const scrollRows = useChatScrollState((s) => s.scrollRows);
  const cardHeights = useChatScrollState((s) => s.cardHeights);
  const { setMaxScroll, setCardHeight, pruneCardHeights } = useChatScrollActions();
  const outerRef = useRef<DOMElement>(null!);
  const innerRef = useRef<DOMElement>(null!);
  const outer = useBoxMetrics(outerRef);
  const inner = useBoxMetrics(innerRef);
  const maxScroll = Math.max(0, inner.height - outer.height);

  useEffect(() => {
    setMaxScroll(maxScroll);
  }, [maxScroll, setMaxScroll]);

  // Drop heights for cards no longer in the list (resumed sessions, /clear, etc).
  useEffect(() => {
    const live = new Set<string>();
    for (const c of cards) live.add(c.id);
    pruneCardHeights(live);
  }, [cards, pruneCardHeights]);

  let visible = cards;
  if (suppressLive && cards.length > 0 && !isFullySettled(cards[cards.length - 1]!)) {
    visible = cards.slice(0, -1);
  }

  /** Compute which cards land inside the visible window + buffer. Cards with
   * unknown heights are always kept live so they get measured on first paint. */
  const items = useMemo(() => {
    const winStart = Math.max(0, scrollRows - VISIBLE_BUFFER_ROWS);
    const winEnd = scrollRows + outer.height + VISIBLE_BUFFER_ROWS;
    const out: Array<{ kind: "spacer"; rows: number; key: string } | { kind: "card"; card: Card }> =
      [];
    let cursor = 0;
    let pendingSpacer = 0;
    let spacerKey = 0;
    for (const card of visible) {
      const h = cardHeights.get(card.id);
      const cardEnd = cursor + (h ?? 0);
      // Render live when:
      //   1. height isn't cached yet (need to measure), OR
      //   2. card range overlaps the visible window.
      const live = h === undefined || (cardEnd >= winStart && cursor <= winEnd);
      if (live) {
        if (pendingSpacer > 0) {
          out.push({ kind: "spacer", rows: pendingSpacer, key: `sp-${spacerKey++}` });
          pendingSpacer = 0;
        }
        out.push({ kind: "card", card });
      } else {
        pendingSpacer += h ?? 0;
      }
      cursor = cardEnd;
    }
    if (pendingSpacer > 0) {
      out.push({ kind: "spacer", rows: pendingSpacer, key: `sp-${spacerKey}` });
    }
    return out;
  }, [visible, cardHeights, scrollRows, outer.height]);

  return (
    <>
      {/* Always reserve the row — making it conditional ties outer.height to scrollRows and closes a setState loop with pinned mode. */}
      <Box height={1} flexShrink={0}>
        {scrollRows > 0 ? <ScrollIndicator scrollRows={scrollRows} maxScroll={maxScroll} /> : null}
      </Box>
      <Box ref={outerRef} flexDirection="column" flexGrow={1} overflow="hidden">
        <Box ref={innerRef} flexDirection="column" marginTop={-scrollRows} flexShrink={0}>
          {items.map((item) =>
            item.kind === "spacer" ? (
              <Box key={item.key} height={item.rows} flexShrink={0} />
            ) : (
              <MeasuredCard key={item.card.id} card={item.card} report={setCardHeight} />
            ),
          )}
        </Box>
      </Box>
    </>
  );
}

/** Thin wrapper that captures a card's row height on every render and reports
 * it to the scroll store. Wrapping in React.memo would defeat the purpose —
 * we *want* the effect to re-run when the streaming card grows. */
function MeasuredCard({
  card,
  report,
}: { card: Card; report: (id: string, rows: number) => void }): React.ReactElement {
  const ref = useRef<DOMElement>(null!);
  const m = useBoxMetrics(ref);
  useEffect(() => {
    if (m.height > 0) report(card.id, m.height);
  }, [card.id, m.height, report]);
  return (
    <Box ref={ref} flexDirection="column" flexShrink={0}>
      <CardRenderer card={card} />
    </Box>
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
  const above =
    scrollRows === 1
      ? t("cardStream.scrollAbove", { scroll: scrollRows, max: maxScroll })
      : t("cardStream.scrollAbovePlural", { scroll: scrollRows, max: maxScroll });
  const more = remaining > 0 ? t("cardStream.scrollMore", { remaining }) : "";
  const text = `${above}${more}${t("cardStream.scrollPgUp")}`;
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
