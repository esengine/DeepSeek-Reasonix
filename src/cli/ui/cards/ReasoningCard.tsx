import { Box, Text, useStdout } from "ink";
// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React from "react";
import { clipToCells, wrapToCells } from "../../../frame/width.js";
import { t } from "../../../i18n/index.js";
import { Card } from "../primitives/Card.js";
import { CardHeader, type MetaItem } from "../primitives/CardHeader.js";
import { CursorBlock } from "../primitives/CursorBlock.js";
import { PILL_MODEL, PILL_SECTION, Pill, modelBadgeFor } from "../primitives/Pill.js";
import { Spinner } from "../primitives/Spinner.js";
import type { ReasoningCard as ReasoningCardData } from "../state/cards.js";
import { FG, TONE, TONE_ACTIVE } from "../theme/tokens.js";

const STREAMING_PREVIEW_LINES = 3;
const SETTLED_HEAD_LINES = 2;
const SETTLED_TAIL_LINES = 2;
/** Above this, head+tail noise > value — collapse to tail + scroll-past summary. */
const XL_TOKEN_THRESHOLD = 800;

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
  const isEmpty = !card.streaming && !card.aborted && allLines.length === 0;
  const showBody = expanded && (allLines.length > 0 || card.streaming || isEmpty);
  const tone = card.aborted ? TONE.err : card.streaming ? TONE_ACTIVE.accent : TONE.accent;

  return (
    <Card tone={tone}>
      <ReasoningHeader card={card} isEmpty={isEmpty} />
      {showBody &&
        (isEmpty ? (
          <EmptyHint />
        ) : card.streaming ? (
          <StreamingPreview card={card} allLines={allLines} lineCells={lineCells} />
        ) : (
          <SettledPreview card={card} allLines={allLines} lineCells={lineCells} />
        ))}
    </Card>
  );
}

function ReasoningHeader({
  card,
  isEmpty,
}: {
  card: ReasoningCardData;
  isEmpty: boolean;
}): React.ReactElement {
  const streamingActive = card.streaming && !card.aborted;
  const headColor = card.aborted
    ? TONE.err
    : streamingActive
      ? TONE_ACTIVE.accent
      : isEmpty
        ? FG.faint
        : TONE.accent;
  const glyph = streamingActive ? "◇" : "◆";
  const title = streamingActive
    ? t("cardTitles.reasoningEllipsis")
    : card.aborted
      ? t("cardTitles.reasoningAborted")
      : t("cardTitles.reasoning");
  const pill = isEmpty ? PILL_SECTION.empty : PILL_SECTION.reason;
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
      titleColor={pill.fg}
      titleBg={pill.bg}
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
    return card.tokens > 0 ? `${card.tokens.toLocaleString()} ${t("cardLabels.tok")}` : "";
  }
  const parts: string[] = [];
  if (card.tokens > 0) parts.push(`${card.tokens.toLocaleString()} ${t("cardLabels.tok")}`);
  if (card.paragraphs > 0) parts.push(`${card.paragraphs} ${t("cardLabels.pilcrow")}`);
  return parts.join(" \u00b7 ");
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
  const hasOverflow = visualLines.length > visible.length;
  return (
    <>
      {hasOverflow ? <Text color={FG.faint}>⋮</Text> : null}
      <BodyLines
        card={card}
        lines={visible}
        lineCells={lineCells}
        anchor={!hasOverflow}
        cursorOnLast
      />
    </>
  );
}

function SettledPreview({ card, allLines, lineCells }: BodyProps): React.ReactElement {
  const visualLines = allLines.flatMap((l) => wrapToCells(l, lineCells));

  if (card.tokens >= XL_TOKEN_THRESHOLD) {
    const visible = visualLines.slice(-SETTLED_TAIL_LINES);
    const droppedLines = Math.max(0, visualLines.length - visible.length);
    return (
      <>
        {droppedLines > 0 ? <ScrollPastHint card={card} /> : null}
        <BodyLines card={card} lines={visible} lineCells={lineCells} indexOffset={droppedLines} />
      </>
    );
  }

  const totalShown = SETTLED_HEAD_LINES + SETTLED_TAIL_LINES;
  if (visualLines.length <= totalShown) {
    return <BodyLines card={card} lines={visualLines} lineCells={lineCells} anchor />;
  }
  const headLines = visualLines.slice(0, SETTLED_HEAD_LINES);
  const tailLines = visualLines.slice(-SETTLED_TAIL_LINES);
  const droppedMid = visualLines.length - headLines.length - tailLines.length;
  return (
    <>
      <BodyLines card={card} lines={headLines} lineCells={lineCells} anchor />
      <MidElisionHint droppedLines={droppedMid} />
      <BodyLines
        card={card}
        lines={tailLines}
        lineCells={lineCells}
        indexOffset={headLines.length + droppedMid}
      />
    </>
  );
}

function EmptyHint(): React.ReactElement {
  return (
    <Text italic color={FG.faint}>
      no thinking — direct answer
    </Text>
  );
}

interface BodyLinesProps {
  card: ReasoningCardData;
  lines: string[];
  lineCells: number;
  cursorOnLast?: boolean;
  indexOffset?: number;
  /** Render ↳ before the first line — only when this slice is the absolute body start. */
  anchor?: boolean;
}

function BodyLines({
  card,
  lines,
  lineCells,
  cursorOnLast = false,
  indexOffset = 0,
  anchor = false,
}: BodyLinesProps): React.ReactElement {
  const tone = card.aborted ? TONE.err : card.streaming ? TONE_ACTIVE.accent : TONE.accent;
  const innerCells = lineCells - (anchor ? 2 : 0);
  return (
    <>
      {lines.map((line, i) => {
        const isLast = i === lines.length - 1;
        const isFirst = i === 0;
        return (
          <Box key={`${card.id}:b:${indexOffset + i}`} flexDirection="row" gap={1}>
            {anchor ? <Text color={tone}>{isFirst ? "↳" : " "}</Text> : null}
            <Text italic color={FG.meta}>
              {clipToCells(line, innerCells)}
            </Text>
            {isLast && cursorOnLast && <CursorBlock />}
          </Box>
        );
      })}
    </>
  );
}

function MidElisionHint({ droppedLines }: { droppedLines: number }): React.ReactElement {
  return (
    <Text color={FG.faint}>{`⋯ ${droppedLines} line${droppedLines === 1 ? "" : "s"} elided`}</Text>
  );
}

function ScrollPastHint({ card }: { card: ReasoningCardData }): React.ReactElement {
  const parts: string[] = [];
  if (card.paragraphs > 0) parts.push(`${card.paragraphs} ¶`);
  if (card.tokens > 0) parts.push(`~${card.tokens.toLocaleString()} tok`);
  return <Text color={FG.faint}>{`⋯ ${parts.join(" + ")} scrolled past · /reasoning last`}</Text>;
}
