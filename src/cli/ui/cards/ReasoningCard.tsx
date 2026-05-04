import { Box, Text, useStdout } from "ink";
// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React from "react";
import { clipToCells, wrapToCells } from "../../../frame/width.js";
import { Card } from "../primitives/Card.js";
import { CardHeader, type MetaItem } from "../primitives/CardHeader.js";
import { CursorBlock } from "../primitives/CursorBlock.js";
import { PILL_MODEL, PILL_SECTION, Pill, modelBadgeFor } from "../primitives/Pill.js";
import { Spinner } from "../primitives/Spinner.js";
import type { ReasoningCard as ReasoningCardData } from "../state/cards.js";
import { FG, TONE, TONE_ACTIVE } from "../theme/tokens.js";

/** Streaming preview tail length — wide enough to feel responsive, small enough not to thrash on every chunk. Full body lives in the events log. */
const STREAMING_PREVIEW_LINES = 4;
/** Once settled, only the conclusion is actionable; the rest is in `/reasoning last`. */
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
  const lineCells = Math.max(20, cols - 4);

  const allLines = card.text.length > 0 ? card.text.split("\n") : [];
  const showBody = expanded && (allLines.length > 0 || card.streaming);
  const tone = card.aborted ? TONE.err : card.streaming ? TONE_ACTIVE.accent : TONE.accent;

  return (
    <Card tone={tone}>
      <ReasoningHeader card={card} />
      {showBody &&
        (card.streaming ? (
          <StreamingPreview card={card} allLines={allLines} lineCells={lineCells} />
        ) : (
          <SettledPreview card={card} allLines={allLines} lineCells={lineCells} />
        ))}
    </Card>
  );
}

function ReasoningHeader({ card }: { card: ReasoningCardData }): React.ReactElement {
  const streamingActive = card.streaming && !card.aborted;
  const headColor = card.aborted ? TONE.err : streamingActive ? TONE_ACTIVE.accent : TONE.accent;
  const glyph = streamingActive ? "◇" : "◆";
  const title = streamingActive ? "reasoning…" : card.aborted ? "reasoning (aborted)" : "reasoning";
  const meta: MetaItem[] = [];
  const m = headerMeta(card);
  if (m) meta.push(m);
  const duration = headerDuration(card);
  if (duration) meta.push(duration);
  const modelBadge = card.model ? modelBadgeFor(card.model) : null;
  return (
    <CardHeader
      glyph={glyph}
      tone={headColor}
      title={title}
      titleColor={PILL_SECTION.reason.fg}
      titleBg={PILL_SECTION.reason.bg}
      meta={meta.length > 0 ? meta : undefined}
      right={
        <>
          {streamingActive ? <Spinner kind="braille" color={TONE_ACTIVE.accent} /> : null}
          {modelBadge ? (
            <Pill label={modelBadge.label} {...PILL_MODEL[modelBadge.kind]} bold={false} />
          ) : null}
        </>
      }
    />
  );
}

function headerMeta(card: ReasoningCardData): string {
  if (card.streaming) {
    return card.tokens > 0 ? `${card.tokens.toLocaleString()} tok` : "";
  }
  const parts: string[] = [];
  if (card.tokens > 0) parts.push(`${card.tokens.toLocaleString()} tok`);
  if (card.paragraphs > 0) parts.push(`${card.paragraphs} ¶`);
  return parts.join(" · ");
}

function headerDuration(card: ReasoningCardData): string {
  if (card.streaming || !card.endedAt) return "";
  const seconds = Math.max(0, (card.endedAt - card.ts) / 1000);
  return `${seconds.toFixed(1)}s`;
}

interface BodyProps {
  card: ReasoningCardData;
  allLines: string[];
  lineCells: number;
}

function StreamingPreview({ card, allLines, lineCells }: BodyProps): React.ReactElement {
  const visualLines = allLines.flatMap((l) => wrapToCells(l, lineCells));
  const visible = visualLines.slice(-STREAMING_PREVIEW_LINES);
  return <BodyLines card={card} lines={visible} lineCells={lineCells} cursorOnLast />;
}

function SettledPreview({ card, allLines, lineCells }: BodyProps): React.ReactElement {
  const visualLines = allLines.flatMap((l) => wrapToCells(l, lineCells));
  const visible = visualLines.slice(-SETTLED_TAIL_LINES);
  const droppedLines = Math.max(0, visualLines.length - visible.length);
  return (
    <>
      {droppedLines > 0 ? <ElisionHint droppedLines={droppedLines} card={card} /> : null}
      <BodyLines card={card} lines={visible} lineCells={lineCells} indexOffset={droppedLines} />
    </>
  );
}

interface BodyLinesProps {
  card: ReasoningCardData;
  lines: string[];
  lineCells: number;
  cursorOnLast?: boolean;
  indexOffset?: number;
}

function BodyLines({
  card,
  lines,
  lineCells,
  cursorOnLast = false,
  indexOffset = 0,
}: BodyLinesProps): React.ReactElement {
  return (
    <>
      {lines.map((line, i) => {
        const isLast = i === lines.length - 1;
        return (
          <Box key={`${card.id}:b:${indexOffset + i}`} flexDirection="row">
            <Text italic color={FG.meta}>
              {clipToCells(line, lineCells)}
            </Text>
            {isLast && cursorOnLast && <CursorBlock />}
          </Box>
        );
      })}
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
  if (card.paragraphs > 1) {
    parts.push(`${card.paragraphs} ¶`);
  } else {
    parts.push(`${droppedLines} line${droppedLines === 1 ? "" : "s"}`);
  }
  if (card.tokens > 0) parts.push(`${card.tokens.toLocaleString()} tok`);
  return <Text color={FG.faint}>{`⋯ ${parts.join(" · ")} above · /reasoning last`}</Text>;
}
