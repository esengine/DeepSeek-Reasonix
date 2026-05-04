import { Box, Text } from "ink";
// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React, { useContext } from "react";
import { ActiveCardContext, Card as CardWrap } from "../primitives/Card.js";
import { CardHeader } from "../primitives/CardHeader.js";
import { Spinner } from "../primitives/Spinner.js";
import type { Card, SubAgentCard as SubAgentCardData } from "../state/cards.js";
import { CARD, FG, TONE, TONE_ACTIVE } from "../theme/tokens.js";

const STATUS_COLOR: Record<SubAgentCardData["status"], string> = {
  running: TONE_ACTIVE.violet,
  done: TONE.ok,
  failed: TONE.err,
};

export function SubAgentCard({ card }: { card: SubAgentCardData }): React.ReactElement {
  const headColor = STATUS_COLOR[card.status];
  const headGlyph = card.status === "failed" ? "✖" : "⌬";
  const runningChildren = card.children.filter((c) => !isChildDone(c)).length;
  const isRunning = card.status === "running";
  const inLive = useContext(ActiveCardContext);
  const headerMeta = isRunning
    ? runningChildren > 0
      ? [`${runningChildren} running`]
      : ["working"]
    : [{ text: card.status, color: headColor }];
  return (
    <CardWrap tone={headColor}>
      <CardHeader
        glyph={headGlyph}
        tone={headColor}
        title="subagent"
        titleColor={TONE.violet}
        subtitle={card.task}
        meta={headerMeta}
      />
      {card.name ? <Text color={FG.faint}>{`agent · ${card.name}`}</Text> : null}
      {card.tools && card.tools.length > 0 && (
        <Text color={FG.faint}>{`tools · ${card.tools.join(", ")}`}</Text>
      )}
      {card.children.map((child) => (
        <Box key={child.id} flexDirection="row" gap={1}>
          {inLive ? null : <Text color={TONE.violet}>▎</Text>}
          <ChildRow card={child} />
        </Box>
      ))}
    </CardWrap>
  );
}

function isChildDone(card: Card): boolean {
  switch (card.kind) {
    case "tool":
    case "streaming":
      return card.done;
    case "reasoning":
      return !card.streaming;
    default:
      return true;
  }
}

interface ChildVisual {
  statusGlyph: React.ReactElement;
  kindGlyph: string;
  kindColor: string;
  text: string;
}

function ChildRow({ card }: { card: Card }): React.ReactElement {
  const v = childVisual(card);
  const isDone = isChildDone(card);
  return (
    <>
      {v.statusGlyph}
      <Text color={v.kindColor}>{v.kindGlyph}</Text>
      <Text dimColor={isDone} color={FG.body}>
        {v.text}
      </Text>
    </>
  );
}

function runningGlyph(color: string): React.ReactElement {
  return <Spinner kind="circle" color={color} />;
}

function doneGlyph(color: string): React.ReactElement {
  return <Text color={color}>✓</Text>;
}

function failedGlyph(): React.ReactElement {
  return <Text color={TONE.err}>✖</Text>;
}

function childVisual(card: Card): ChildVisual {
  switch (card.kind) {
    case "reasoning": {
      const done = !card.streaming;
      return {
        statusGlyph: done ? doneGlyph(TONE.ok) : runningGlyph(CARD.reasoning.color),
        kindGlyph: "◆",
        kindColor: CARD.reasoning.color,
        text: `reasoning · ${card.paragraphs} ¶`,
      };
    }
    case "tool": {
      const elapsed = card.elapsedMs > 0 ? ` · ${(card.elapsedMs / 1000).toFixed(2)}s` : "";
      return {
        statusGlyph: card.done ? doneGlyph(TONE.ok) : runningGlyph(CARD.tool.color),
        kindGlyph: "▣",
        kindColor: CARD.tool.color,
        text: `${card.name}${elapsed}`,
      };
    }
    case "streaming":
      return {
        statusGlyph: card.done ? doneGlyph(TONE.ok) : runningGlyph(CARD.streaming.color),
        kindGlyph: "◈",
        kindColor: CARD.streaming.color,
        text: card.done ? "response" : "writing …",
      };
    case "diff":
      return {
        statusGlyph: doneGlyph(TONE.ok),
        kindGlyph: "±",
        kindColor: CARD.diff.color,
        text: card.file,
      };
    case "error":
      return {
        statusGlyph: failedGlyph(),
        kindGlyph: "✖",
        kindColor: CARD.error.color,
        text: card.title,
      };
    default:
      return {
        statusGlyph: <Text color={FG.faint}>·</Text>,
        kindGlyph: "·",
        kindColor: FG.faint,
        text: card.kind,
      };
  }
}
