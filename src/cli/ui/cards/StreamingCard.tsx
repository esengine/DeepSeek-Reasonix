import { Box, Text, useStdout } from "ink";
// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React from "react";
import { clipToCells, wrapToCells } from "../../../frame/width.js";
import { useReserveRows } from "../layout/viewport-budget.js";
import { Markdown } from "../markdown.js";
import { Spinner } from "../primitives/Spinner.js";
import type { StreamingCard as StreamingCardData } from "../state/cards.js";
import { FG, TONE } from "../theme/tokens.js";

const BODY_PAD = 2;
/** Streaming preview tail length — bounded live region so chunks don't thrash whole-card layout. */
const STREAMING_PREVIEW_LINES = 4;

export function StreamingCard({ card }: { card: StreamingCardData }): React.ReactElement {
  const { stdout } = useStdout();
  const cols = stdout?.columns ?? 80;
  useReserveRows("stream", {
    min: STREAMING_PREVIEW_LINES + 1,
    max: STREAMING_PREVIEW_LINES + 2,
  });

  if (card.done && !card.aborted) {
    return (
      <Box flexDirection="column" marginTop={1}>
        <Box flexDirection="row" gap={1}>
          <Text color={TONE.ok}>‹</Text>
          <Text color={TONE.ok} bold>
            reply
          </Text>
        </Box>
        <Box paddingLeft={BODY_PAD} flexDirection="column">
          <Markdown text={card.text} />
        </Box>
      </Box>
    );
  }

  const lineCells = Math.max(20, cols - BODY_PAD - 4);
  const allLines = card.text.length > 0 ? card.text.split("\n") : [""];
  const visualLines = allLines.flatMap((l) => wrapToCells(l, lineCells));
  const visible = visualLines.slice(-STREAMING_PREVIEW_LINES);
  const aborted = !!card.aborted;
  const headColor = aborted ? TONE.err : TONE.brand;
  const glyph = aborted ? "‹" : "▸";
  const headLabel = aborted ? "aborted" : "writing…";

  return (
    <Box flexDirection="column" marginTop={1}>
      <Box flexDirection="row" gap={1}>
        <Text color={headColor}>{glyph}</Text>
        <Text color={headColor} bold>
          {headLabel}
        </Text>
        {aborted ? null : (
          <Box flexDirection="row">
            <Spinner kind="braille" color={TONE.brand} />
          </Box>
        )}
      </Box>
      {visible.map((line, i) => (
        <Box
          key={`${card.id}:${allLines.length - visible.length + i}`}
          paddingLeft={BODY_PAD}
          flexDirection="row"
        >
          <Text color={aborted ? FG.meta : FG.body}>{clipToCells(line, lineCells)}</Text>
        </Box>
      ))}
      {aborted ? (
        <Box paddingLeft={BODY_PAD}>
          <Text color={FG.faint}>[truncated by esc]</Text>
        </Box>
      ) : null}
    </Box>
  );
}
