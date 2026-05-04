import { Box, Text } from "ink";
// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React from "react";
import type { Card, SubAgentCard as SubAgentCardData } from "../state/cards.js";
import { FG, TONE } from "../theme/tokens.js";
import { CardLayout } from "./CardLayout.js";

const STATUS_COLOR: Record<SubAgentCardData["status"], string> = {
  running: TONE.violet,
  done: TONE.ok,
  failed: TONE.err,
};

export function SubAgentCard({ card }: { card: SubAgentCardData }): React.ReactElement {
  return (
    <CardLayout
      glyph="⌬"
      tone={TONE.violet}
      title={`subagent · ${card.name}`}
      trailing={<Text color={STATUS_COLOR[card.status]}>{card.status}</Text>}
    >
      <Box flexDirection="row" gap={1}>
        <Text color={FG.faint}>task</Text>
        <Text color={FG.sub}>{card.task}</Text>
      </Box>
      {card.tools && card.tools.length > 0 ? (
        <Box flexDirection="row" gap={1}>
          <Text color={FG.faint}>tools</Text>
          <Text color={FG.sub}>{card.tools.join(", ")}</Text>
        </Box>
      ) : null}
      {card.children.length > 0 ? (
        <Box flexDirection="column" marginTop={1}>
          <Text color={FG.meta}>sub-agent stream</Text>
          {card.children.map((child) => (
            <Box key={child.id} flexDirection="row" gap={1}>
              <Text color={TONE.violet}>▎</Text>
              <ChildSummary card={child} />
            </Box>
          ))}
        </Box>
      ) : null}
    </CardLayout>
  );
}

function ChildSummary({ card }: { card: Card }): React.ReactElement {
  switch (card.kind) {
    case "reasoning":
      return (
        <>
          <Text color={TONE.accent}>◆</Text>
          <Text italic color={FG.meta}>
            {`reasoning · ${card.paragraphs} paragraph${card.paragraphs === 1 ? "" : "s"}`}
          </Text>
        </>
      );
    case "tool":
      return (
        <>
          <Text color={TONE.brand}>▣</Text>
          <Text bold color={FG.body}>
            {card.name}
          </Text>
          {card.elapsedMs > 0 ? (
            <Text color={FG.faint}>{`${(card.elapsedMs / 1000).toFixed(2)}s`}</Text>
          ) : null}
        </>
      );
    case "streaming":
      return (
        <>
          <Text color={TONE.brand}>▶</Text>
          <Text color={card.done ? FG.sub : TONE.brand}>
            {card.done ? "response" : "streaming response …"}
          </Text>
        </>
      );
    case "diff":
      return (
        <>
          <Text color={TONE.ok}>±</Text>
          <Text color={FG.sub}>{card.file}</Text>
        </>
      );
    case "error":
      return (
        <>
          <Text color={TONE.err}>✖</Text>
          <Text color={FG.sub}>{card.title}</Text>
        </>
      );
    default:
      return <Text color={FG.faint}>{card.kind}</Text>;
  }
}
