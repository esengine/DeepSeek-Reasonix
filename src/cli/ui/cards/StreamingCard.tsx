import { Box, Text, useStdout } from "ink";
// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React from "react";
import { clipToCells, wrapToCells } from "../../../frame/width.js";
import { useReserveRows } from "../layout/viewport-budget.js";
import { Markdown } from "../markdown.js";
import { Card } from "../primitives/Card.js";
import { CardHeader } from "../primitives/CardHeader.js";
import { PILL_MODEL, Pill, modelBadgeFor } from "../primitives/Pill.js";
import { Spinner } from "../primitives/Spinner.js";
import type { StreamingCard as StreamingCardData } from "../state/cards.js";
import { FG, TONE, TONE_ACTIVE } from "../theme/tokens.js";

/** Streaming preview tail length — bounded live region so chunks don't thrash whole-card layout. */
const STREAMING_PREVIEW_LINES = 4;

export function StreamingCard({ card }: { card: StreamingCardData }): React.ReactElement {
  const { stdout } = useStdout();
  const cols = stdout?.columns ?? 80;
  useReserveRows("stream", {
    min: STREAMING_PREVIEW_LINES + 1,
    max: STREAMING_PREVIEW_LINES + 2,
  });

  const modelBadge = card.model ? modelBadgeFor(card.model) : null;
  const modelPill = modelBadge ? (
    <Pill label={modelBadge.label} {...PILL_MODEL[modelBadge.kind]} bold={false} />
  ) : null;

  if (card.done && !card.aborted) {
    return (
      <Card tone={TONE.ok}>
        <CardHeader glyph="‹" tone={TONE.ok} title="reply" right={modelPill} />
        <Markdown text={card.text} />
      </Card>
    );
  }

  const lineCells = Math.max(20, cols - 4);
  const allLines = card.text.length > 0 ? card.text.split("\n") : [""];
  const visualLines = allLines.flatMap((l) => wrapToCells(l, lineCells));
  const visible = visualLines.slice(-STREAMING_PREVIEW_LINES);
  const aborted = !!card.aborted;
  const headColor = aborted ? TONE.err : TONE_ACTIVE.brand;
  const glyph = aborted ? "‹" : "◈";
  const headLabel = aborted ? "aborted" : "writing…";

  return (
    <Card tone={headColor}>
      <CardHeader
        glyph={glyph}
        tone={headColor}
        title={headLabel}
        right={
          <>
            {aborted ? null : <Spinner kind="braille" color={TONE_ACTIVE.brand} />}
            {modelPill}
          </>
        }
      />
      {visible.map((line, i) => (
        <Box key={`${card.id}:${allLines.length - visible.length + i}`} flexDirection="row">
          <Text color={aborted ? FG.meta : FG.body}>{clipToCells(line, lineCells)}</Text>
        </Box>
      ))}
      {aborted ? <Text color={FG.faint}>[truncated by esc]</Text> : null}
    </Card>
  );
}
