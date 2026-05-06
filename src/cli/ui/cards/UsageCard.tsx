import { Box, Text } from "ink";
// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React from "react";
import { Card } from "../primitives/Card.js";
import { CardHeader } from "../primitives/CardHeader.js";
import type { UsageCard as UsageCardData } from "../state/cards.js";
import { FG, TONE, formatBalance, formatCost } from "../theme/tokens.js";

const BAR_CELLS = 30;

function compactNum(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(n >= 10_000 ? 0 : 1)}K`;
  return String(n);
}

function bar(ratio: number, color: string): React.ReactElement {
  const filled = Math.max(0, Math.min(BAR_CELLS, Math.round(ratio * BAR_CELLS)));
  const empty = BAR_CELLS - filled;
  return (
    <>
      <Text color={color}>{"█".repeat(filled)}</Text>
      <Text color={FG.faint}>{"░".repeat(empty)}</Text>
    </>
  );
}

export function UsageCard({ card }: { card: UsageCardData }): React.ReactElement {
  if (card.compact) return <CompactUsageRow card={card} />;
  const cap = Math.max(1, card.tokens.promptCap);
  const promptRatio = card.tokens.prompt / cap;
  const reasonRatio = card.tokens.reason / cap;
  const outputRatio = card.tokens.output / cap;

  const headerMeta: string[] = [`turn ${card.turn}`, formatCost(card.cost, card.balanceCurrency)];
  if (card.elapsedMs !== undefined) headerMeta.push(`${(card.elapsedMs / 1000).toFixed(1)}s`);
  return (
    <Card tone={FG.meta}>
      <CardHeader glyph="Σ" tone={FG.meta} title="usage" meta={headerMeta} />
      <Box flexDirection="row" gap={1}>
        <Text color={FG.sub}>prompt</Text>
        {bar(promptRatio, TONE.brand)}
        <Text bold color={FG.body}>
          {card.tokens.prompt.toLocaleString()}
        </Text>
        <Text color={FG.faint}>{`/ 1M · ${(promptRatio * 100).toFixed(1)}%`}</Text>
      </Box>
      <Box flexDirection="row" gap={1}>
        <Text color={FG.sub}>reason</Text>
        {bar(reasonRatio, TONE.accent)}
        <Text bold color={FG.body}>
          {card.tokens.reason.toLocaleString()}
        </Text>
      </Box>
      <Box flexDirection="row" gap={1}>
        <Text color={FG.sub}>output</Text>
        {bar(outputRatio, TONE.brand)}
        <Text bold color={FG.body}>
          {card.tokens.output.toLocaleString()}
        </Text>
      </Box>
      <Box flexDirection="row" gap={1}>
        <Text color={FG.sub}>cache </Text>
        {bar(card.cacheHit, TONE.ok)}
        <Text bold color={TONE.ok}>{`${(card.cacheHit * 100).toFixed(1)}%`}</Text>
      </Box>
      <Box flexDirection="row" gap={1}>
        <Text color={FG.faint}>session</Text>
        <Text bold color={FG.body}>
          {`⛁ ${formatCost(card.sessionCost, card.balanceCurrency, 3)}`}
        </Text>
        {card.balance !== undefined ? (
          <>
            <Text color={FG.faint}>· balance</Text>
            <Text bold color={TONE.brand}>
              {formatBalance(card.balance, card.balanceCurrency)}
            </Text>
          </>
        ) : null}
      </Box>
    </Card>
  );
}

function CompactUsageRow({ card }: { card: UsageCardData }): React.ReactElement {
  const elapsed = card.elapsedMs !== undefined ? ` · ${(card.elapsedMs / 1000).toFixed(1)}s` : "";
  return (
    <Box flexDirection="row" gap={1} marginTop={1}>
      <Text color={FG.meta}>Σ</Text>
      <Text color={FG.faint}>{`turn ${card.turn}`}</Text>
      <Text color={FG.meta}>
        {`· ${compactNum(card.tokens.prompt)} prompt · ${compactNum(card.tokens.output)} out`}
      </Text>
      <Text color={FG.faint}>· cache</Text>
      <Text color={TONE.ok}>{`${(card.cacheHit * 100).toFixed(0)}%`}</Text>
      <Text color={FG.faint}>{`· ${formatCost(card.cost, card.balanceCurrency)}${elapsed}`}</Text>
      {card.balance !== undefined ? (
        <Text color={TONE.brand}>{`· ${formatBalance(card.balance, card.balanceCurrency)}`}</Text>
      ) : null}
    </Box>
  );
}
