import { Box, Text } from "ink";
// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React, { useContext } from "react";
import { t } from "../../../i18n/index.js";
import { ActiveCardContext, Card as CardWrap } from "../primitives/Card.js";
import { CardHeader } from "../primitives/CardHeader.js";
import { Spinner } from "../primitives/Spinner.js";
import type { Card, SubAgentCard as SubAgentCardData } from "../state/cards.js";
import { useThemeTokens } from "../theme/context.js";
import { CARD } from "../theme/tokens.js";

export function SubAgentCard({ card }: { card: SubAgentCardData }): React.ReactElement {
  const { fg, tone, toneActive } = useThemeTokens();
  const statusColor: Record<SubAgentCardData["status"], string> = {
    running: toneActive.violet,
    done: tone.ok,
    failed: tone.err,
  };
  const headColor = statusColor[card.status];
  const headGlyph = card.status === "failed" ? "\u2717" : "\u232c";
  const runningChildren = card.children.filter((c) => !isChildDone(c)).length;
  const isRunning = card.status === "running";
  const inLive = useContext(ActiveCardContext);
  const headerMeta = isRunning
    ? runningChildren > 0
      ? [`${runningChildren} ${t("cardLabels.runningLabel")}`]
      : [t("cardLabels.workingLabel")]
    : [{ text: card.status, color: headColor }];
  return (
    <CardWrap tone={headColor}>
      <CardHeader
        glyph={headGlyph}
        tone={headColor}
        title={t("cardTitles.subagent")}
        titleColor={tone.violet}
        subtitle={card.task}
        meta={headerMeta}
      />
      {card.name ? <Text color={fg.faint}>{`${t("cardLabels.agent")} · ${card.name}`}</Text> : null}
      {card.tools && card.tools.length > 0 && (
        <Text color={fg.faint}>{`${t("cardLabels.tools")} · ${card.tools.join(", ")}`}</Text>
      )}
      {card.children.map((child) => (
        <Box key={child.id} flexDirection="row" gap={1}>
          {inLive ? null : <Text color={tone.violet}>▎</Text>}
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
  const { fg, tone } = useThemeTokens();
  const v = childVisual(card, tone.ok, tone.err, fg.faint);
  const isDone = isChildDone(card);
  return (
    <>
      {v.statusGlyph}
      <Text color={v.kindColor}>{v.kindGlyph}</Text>
      <Text dimColor={isDone} color={fg.body}>
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

function failedGlyph(color: string): React.ReactElement {
  return <Text color={color}>✖</Text>;
}

function childVisual(
  card: Card,
  doneColor: string,
  failedColor: string,
  fallbackColor: string,
): ChildVisual {
  switch (card.kind) {
    case "reasoning": {
      const done = !card.streaming;
      return {
        statusGlyph: done ? doneGlyph(doneColor) : runningGlyph(CARD.reasoning.color),
        kindGlyph: "◆",
        kindColor: CARD.reasoning.color,
        text: t("cardLabels.reasoningLabel", { count: card.paragraphs }),
      };
    }
    case "tool": {
      const elapsed = card.elapsedMs > 0 ? ` · ${(card.elapsedMs / 1000).toFixed(2)}s` : "";
      return {
        statusGlyph: card.done ? doneGlyph(doneColor) : runningGlyph(CARD.tool.color),
        kindGlyph: "▣",
        kindColor: CARD.tool.color,
        text: `${card.name}${elapsed}`,
      };
    }
    case "streaming":
      return {
        statusGlyph: card.done ? doneGlyph(doneColor) : runningGlyph(CARD.streaming.color),
        kindGlyph: "◈",
        kindColor: CARD.streaming.color,
        text: card.done ? t("cardLabels.response") : t("cardLabels.writing"),
      };
    case "diff":
      return {
        statusGlyph: doneGlyph(doneColor),
        kindGlyph: "±",
        kindColor: CARD.diff.color,
        text: card.file,
      };
    case "error":
      return {
        statusGlyph: failedGlyph(failedColor),
        kindGlyph: "✖",
        kindColor: CARD.error.color,
        text: card.title,
      };
    default:
      return {
        statusGlyph: <Text color={fallbackColor}>·</Text>,
        kindGlyph: "·",
        kindColor: fallbackColor,
        text: card.kind,
      };
  }
}
