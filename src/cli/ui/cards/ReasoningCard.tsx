import { Box, Text, useStdout } from "ink";
// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React from "react";
import { clipToCells, wrapToCells } from "../../../frame/width.js";
import { Spinner } from "../primitives/Spinner.js";
import type { ReasoningCard as ReasoningCardData } from "../state/cards.js";
import { FG, TONE } from "../theme/tokens.js";
import { CardLayout } from "./CardLayout.js";
import { CursorBlock } from "./CursorBlock.js";

const STREAMING_PREVIEW_LINES = 4;
const SETTLED_TAIL_LINES = 2;

export function ReasoningCard({
  card,
  expanded,
}: {
  card: ReasoningCardData;
  expanded: boolean;
}): React.ReactElement {
  const { stdout } = useStdout();
  const cols = stdout?.columns ?? 80;
  const lineCells = Math.max(20, cols - 6);
  const allLines = card.text.length > 0 ? card.text.split("\n") : [];
  const showBody = expanded && (allLines.length > 0 || card.streaming);
  const streamingActive = card.streaming && !card.aborted;
  const tone = card.aborted ? TONE.err : TONE.accent;
  const glyph = streamingActive ? "◇" : "◆";
  const title = streamingActive ? "reasoning…" : card.aborted ? "reasoning (aborted)" : "reasoning";

  return (
    <CardLayout
      glyph={glyph}
      tone={tone}
      title={title}
      meta={headerMeta(card, streamingActive)}
      trailing={streamingActive ? <Spinner kind="braille" color={TONE.accent} /> : undefined}
    >
      {showBody
        ? streamingActive
          ? renderStreamingTail(card, allLines, lineCells)
          : renderSettledTail(card, allLines, lineCells)
        : null}
    </CardLayout>
  );
}

function headerMeta(card: ReasoningCardData, streaming: boolean): string {
  const parts: string[] = [];
  if (card.tokens > 0) parts.push(`${card.tokens.toLocaleString()} tok`);
  if (!streaming && card.paragraphs > 0) parts.push(`${card.paragraphs} ¶`);
  if (!streaming && card.endedAt) {
    parts.push(`${Math.max(0, (card.endedAt - card.ts) / 1000).toFixed(1)}s`);
  }
  return parts.join(" · ");
}

function renderStreamingTail(
  card: ReasoningCardData,
  allLines: ReadonlyArray<string>,
  lineCells: number,
): React.ReactNode {
  const visualLines = allLines.flatMap((l) => wrapToCells(l, lineCells));
  const visible = visualLines.slice(-STREAMING_PREVIEW_LINES);
  return visible.map((line, i) => {
    const isLast = i === visible.length - 1;
    return (
      <Box key={`${card.id}:s:${i}`} flexDirection="row">
        <Text italic color={FG.meta}>
          {clipToCells(line, lineCells)}
        </Text>
        {isLast ? <CursorBlock /> : null}
      </Box>
    );
  });
}

function renderSettledTail(
  card: ReasoningCardData,
  allLines: ReadonlyArray<string>,
  lineCells: number,
): React.ReactNode {
  const visualLines = allLines.flatMap((l) => wrapToCells(l, lineCells));
  const visible = visualLines.slice(-SETTLED_TAIL_LINES);
  const dropped = Math.max(0, visualLines.length - visible.length);
  return (
    <>
      {dropped > 0 ? <ElisionHint droppedLines={dropped} card={card} /> : null}
      {visible.map((line, i) => (
        <Text key={`${card.id}:t:${dropped + i}`} italic color={FG.meta}>
          {clipToCells(line, lineCells)}
        </Text>
      ))}
    </>
  );
}

function ElisionHint({
  droppedLines,
  card,
}: {
  droppedLines: number;
  card: ReasoningCardData;
}): React.ReactElement {
  const parts: string[] = [];
  if (card.paragraphs > 1) parts.push(`${card.paragraphs} ¶`);
  else parts.push(`${droppedLines} line${droppedLines === 1 ? "" : "s"}`);
  if (card.tokens > 0) parts.push(`${card.tokens.toLocaleString()} tok`);
  return <Text color={FG.faint}>{`⋯ ${parts.join(" · ")} above · /reasoning last`}</Text>;
}
