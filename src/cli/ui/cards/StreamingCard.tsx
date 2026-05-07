import { Box, Text, useStdout } from "ink";
// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React, { useContext } from "react";
import { clipToCells, wrapToCells } from "../../../frame/width.js";
import { countTokens } from "../../../tokenizer.js";
import { LiveExpandContext } from "../layout/LiveExpandContext.js";
import { useReserveRows } from "../layout/viewport-budget.js";
import { Markdown } from "../markdown.js";
import { Card } from "../primitives/Card.js";
import { CardHeader } from "../primitives/CardHeader.js";
import { PILL_MODEL, Pill, modelBadgeFor } from "../primitives/Pill.js";
import { Spinner } from "../primitives/Spinner.js";
import type { StreamingCard as StreamingCardData } from "../state/cards.js";
import { FG, TONE, TONE_ACTIVE } from "../theme/tokens.js";
import { useSlowTick } from "../ticker.js";

/** Streaming preview tail length — bounded live region so chunks don't thrash whole-card layout. */
const STREAMING_PREVIEW_LINES = 4;
/** Expanded mode shows up to this many lines so the card can't swallow the whole viewport. */
const EXPANDED_MAX_LINES = 60;

const MIN_ELAPSED_MS_FOR_RATE = 500;
const MIN_TOKENS_FOR_RATE = 4;

function formatTokenCount(n: number): string {
  if (n >= 10000) return `${(n / 1000).toFixed(1)}k`;
  if (n >= 1000) return `${(n / 1000).toFixed(2)}k`;
  return String(n);
}

function tokenRate(
  text: string,
  startTs: number,
  endTs: number,
): { tokens: number; tps: number | null } {
  const tokens = countTokens(text);
  const elapsedMs = endTs - startTs;
  if (elapsedMs < MIN_ELAPSED_MS_FOR_RATE || tokens < MIN_TOKENS_FOR_RATE) {
    return { tokens, tps: null };
  }
  return { tokens, tps: Math.round((tokens * 1000) / elapsedMs) };
}

const PILL_RATE = { bg: "#11141a", fg: "#8b949e" } as const;

export function StreamingCard({ card }: { card: StreamingCardData }): React.ReactElement {
  const { stdout } = useStdout();
  const cols = stdout?.columns ?? 80;
  const expanded = useContext(LiveExpandContext);
  const reserveCap = expanded ? EXPANDED_MAX_LINES + 2 : STREAMING_PREVIEW_LINES + 2;
  useReserveRows("stream", {
    min: STREAMING_PREVIEW_LINES + 1,
    max: reserveCap,
  });
  // Re-render at 1Hz so the rate keeps updating even when chunks stall.
  // Frozen once `card.done` is true — settled cards render via Static.
  useSlowTick();

  const modelBadge = card.model ? modelBadgeFor(card.model) : null;
  const modelPill = modelBadge ? (
    <Pill label={modelBadge.label} {...PILL_MODEL[modelBadge.kind]} bold={false} />
  ) : null;

  if (card.done && !card.aborted) {
    const { tokens, tps } = tokenRate(card.text, card.ts, card.endedAt ?? Date.now());
    const ratePill =
      tokens >= MIN_TOKENS_FOR_RATE && tps !== null ? (
        <Pill label={`${formatTokenCount(tokens)} tok · ${tps} t/s`} {...PILL_RATE} bold={false} />
      ) : null;
    return (
      <Card tone={TONE.ok}>
        <CardHeader
          glyph="‹"
          tone={TONE.ok}
          title="reply"
          right={
            <>
              {ratePill}
              {modelPill}
            </>
          }
        />
        <Markdown text={card.text} />
      </Card>
    );
  }

  const lineCells = Math.max(20, cols - 4);
  const allLines = card.text.length > 0 ? card.text.split("\n") : [""];
  const visualLines = allLines.flatMap((l) => wrapToCells(l, lineCells));
  const cap = expanded ? EXPANDED_MAX_LINES : STREAMING_PREVIEW_LINES;
  const visible = visualLines.slice(-cap);
  const droppedAbove = Math.max(0, visualLines.length - visible.length);
  const aborted = !!card.aborted;
  const headColor = aborted ? TONE.err : TONE_ACTIVE.brand;
  const glyph = aborted ? "‹" : "◈";
  const headLabel = aborted ? "aborted" : "writing…";

  const { tokens: liveTokens, tps: liveTps } = tokenRate(card.text, card.ts, Date.now());
  const liveRatePill =
    !aborted && liveTokens >= MIN_TOKENS_FOR_RATE && liveTps !== null ? (
      <Pill label={`${liveTps} t/s`} {...PILL_RATE} bold={false} />
    ) : null;
  const expandPill = !aborted ? (
    <Pill label={expanded ? "expanded ⌃o" : "preview ⌃o"} {...PILL_RATE} bold={false} />
  ) : null;

  return (
    <Card tone={headColor}>
      <CardHeader
        glyph={glyph}
        tone={headColor}
        title={headLabel}
        right={
          <>
            {liveRatePill}
            {expandPill}
            {aborted ? null : <Spinner kind="braille" color={TONE_ACTIVE.brand} />}
            {modelPill}
          </>
        }
      />
      {expanded && droppedAbove > 0 ? (
        <Text
          color={FG.faint}
        >{`⋯ ${droppedAbove} earlier line${droppedAbove === 1 ? "" : "s"} above`}</Text>
      ) : null}
      {visible.map((line, i) => (
        <Box key={`${card.id}:${visualLines.length - visible.length + i}`} flexDirection="row">
          <Text color={aborted ? FG.meta : FG.body}>{clipToCells(line, lineCells)}</Text>
        </Box>
      ))}
      {aborted ? <Text color={FG.faint}>[truncated by esc]</Text> : null}
    </Card>
  );
}
