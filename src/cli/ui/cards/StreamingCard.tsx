import { Text, useStdout } from "ink";
// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React from "react";
import { clipToCells, wrapToCells } from "../../../frame/width.js";
import { useReserveRows } from "../layout/viewport-budget.js";
import { Markdown } from "../markdown.js";
import { Spinner } from "../primitives/Spinner.js";
import type { StreamingCard as StreamingCardData } from "../state/cards.js";
import { FG, TONE } from "../theme/tokens.js";
import { CardLayout } from "./CardLayout.js";

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
      <CardLayout glyph="‹" tone={TONE.ok} title="reply">
        <Markdown text={card.text} />
      </CardLayout>
    );
  }

  const lineCells = Math.max(20, cols - 6);
  const allLines = card.text.length > 0 ? card.text.split("\n") : [""];
  const visualLines = allLines.flatMap((l) => wrapToCells(l, lineCells));
  const visible = visualLines.slice(-STREAMING_PREVIEW_LINES);
  const aborted = !!card.aborted;

  return (
    <CardLayout
      glyph={aborted ? "‹" : "▸"}
      tone={aborted ? TONE.err : TONE.brand}
      title={aborted ? "aborted" : "writing…"}
      trailing={aborted ? undefined : <Spinner kind="braille" color={TONE.brand} />}
    >
      {visible.map((line, i) => (
        <Text
          key={`${card.id}:${allLines.length - visible.length + i}`}
          color={aborted ? FG.meta : FG.body}
        >
          {clipToCells(line, lineCells) || " "}
        </Text>
      ))}
      {aborted ? <Text color={FG.faint}>[truncated by esc]</Text> : null}
    </CardLayout>
  );
}
