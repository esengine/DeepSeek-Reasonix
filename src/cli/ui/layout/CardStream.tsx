import { Box, type DOMElement, Text, useBoxMetrics, useStdout } from "ink";
import React, { useEffect, useMemo, useRef } from "react";
import stringWidth from "string-width";
import { t } from "../../../i18n/index.js";
import { CardRenderer } from "../cards/CardRenderer.js";
import type { Card } from "../state/cards.js";
import { useChatScrollActions, useChatScrollState } from "../state/chat-scroll-provider.js";
import { useAgentState } from "../state/provider.js";
import { FG, SURFACE, TONE } from "../theme/tokens.js";

export const VISIBLE_BUFFER_ROWS = 30;

export type CardStreamItem<T> =
  | { kind: "spacer"; rows: number; key: string }
  | { kind: "card"; card: T };

export function computeCardStreamItems<T extends { id: string }>(
  cards: readonly T[],
  cardHeights: ReadonlyMap<string, number>,
  scrollRows: number,
  outerHeight: number,
): CardStreamItem<T>[] {
  const bucket = Math.floor(scrollRows / VISIBLE_BUFFER_ROWS) * VISIBLE_BUFFER_ROWS;
  const winStart = Math.max(0, bucket - VISIBLE_BUFFER_ROWS);
  const winEnd = bucket + outerHeight + VISIBLE_BUFFER_ROWS * 2;
  const out: CardStreamItem<T>[] = [];
  let cursor = 0;
  let pendingSpacer = 0;
  let spacerKey = 0;
  for (const card of cards) {
    const h = cardHeights.get(card.id);
    const cardEnd = cursor + (h ?? 0);
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
  if (pendingSpacer > 0) out.push({ kind: "spacer", rows: pendingSpacer, key: `sp-${spacerKey}` });
  return out;
}

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

  useEffect(() => {
    pruneCardHeights(new Set(cards.map((c) => c.id)));
  }, [cards, pruneCardHeights]);

  let visible = cards;
  if (suppressLive && cards.length > 0 && !isFullySettled(cards[cards.length - 1]!)) {
    visible = cards.slice(0, -1);
  }

  const items = useMemo(
    () => computeCardStreamItems(visible, cardHeights, scrollRows, outer.height),
    [visible, cardHeights, scrollRows, outer.height],
  );

  return (
    <>
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

function MeasuredCard({
  card,
  report,
}: {
  card: Card;
  report: (id: string, rows: number) => void;
}): React.ReactElement {
  const ref = useRef<DOMElement>(null!);
  const metrics = useBoxMetrics(ref);
  const lastReportedRef = useRef<number>(0);
  const settled = isFullySettled(card);

  useEffect(() => {
    const rows = metrics.height;
    if (rows <= 0 || rows === lastReportedRef.current) return;
    if (!settled && rows < lastReportedRef.current) return;
    lastReportedRef.current = rows;
    report(card.id, rows);
  }, [card.id, metrics.height, report, settled]);

  return (
    <Box ref={ref} flexDirection="column" flexShrink={0}>
      <CardRenderer card={card} />
    </Box>
  );
}

function ScrollIndicator({
  scrollRows,
  maxScroll,
}: {
  scrollRows: number;
  maxScroll: number;
}): React.ReactElement {
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
  const text = `${above}${more}${t("cardStream.scrollPgUp")}${t("cardStream.scrollCopy")}`;
  const { stdout } = useStdout();
  const cols = stdout?.columns ?? 80;
  const pad = Math.max(0, cols - stringWidth(text));
  return (
    <Text color={hot ? TONE.accent : FG.faint} backgroundColor={SURFACE.bgElev}>
      {text + " ".repeat(pad)}
    </Text>
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
