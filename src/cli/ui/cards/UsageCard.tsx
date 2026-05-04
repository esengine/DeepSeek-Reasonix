import { Box, Text } from "ink";
// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React from "react";
import type { UsageCard as UsageCardData } from "../state/cards.js";
import { FG, TONE, formatCNY } from "../theme/tokens.js";
import { CardLayout } from "./CardLayout.js";
import { ProgressBar } from "./ProgressBar.js";

const BAR_CELLS = 30;

function compactNum(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(n >= 10_000 ? 0 : 1)}K`;
  return String(n);
}

function Row({
  label,
  ratio,
  color,
  tokens,
  trail,
}: {
  label: string;
  ratio: number;
  color: string;
  tokens: number;
  trail?: string;
}): React.ReactElement {
  return (
    <Box flexDirection="row" gap={1}>
      <Text color={FG.sub}>{label.padEnd(8)}</Text>
      <ProgressBar ratio={ratio} color={color} cells={BAR_CELLS} />
      <Text bold color={FG.body}>
        {tokens.toLocaleString()}
      </Text>
      {trail ? <Text color={FG.faint}>{trail}</Text> : null}
    </Box>
  );
}

export function UsageCard({ card }: { card: UsageCardData }): React.ReactElement {
  if (card.compact) return <CompactUsageRow card={card} />;
  const cap = Math.max(1, card.tokens.promptCap);
  const promptRatio = card.tokens.prompt / cap;
  const reasonRatio = card.tokens.reason / cap;
  const outputRatio = card.tokens.output / cap;
  const elapsed = card.elapsedMs !== undefined ? ` · ${(card.elapsedMs / 1000).toFixed(1)}s` : "";
  const meta = `turn ${card.turn} · ${formatCNY(card.cost)}${elapsed}`;

  return (
    <CardLayout glyph="Σ" tone={TONE.brand} title="usage" meta={meta}>
      <Row
        label="prompt"
        ratio={promptRatio}
        color={TONE.brand}
        tokens={card.tokens.prompt}
        trail={`/ 1M · ${(promptRatio * 100).toFixed(1)}%`}
      />
      <Row label="reason" ratio={reasonRatio} color={TONE.accent} tokens={card.tokens.reason} />
      <Row label="output" ratio={outputRatio} color={TONE.brand} tokens={card.tokens.output} />
      <Box marginTop={1}>
        <Row
          label="cache"
          ratio={card.cacheHit}
          color={TONE.ok}
          tokens={Math.round(card.cacheHit * 100)}
          trail="%"
        />
      </Box>
      <Box flexDirection="row" gap={1} marginTop={1}>
        <Text color={FG.faint}>session</Text>
        <Text bold color={FG.body}>{`⛁ ${formatCNY(card.sessionCost, 3)}`}</Text>
        {card.balance !== undefined ? (
          <>
            <Text color={FG.faint}>· balance</Text>
            <Text bold color={TONE.brand}>{`¥${card.balance.toFixed(2)}`}</Text>
          </>
        ) : null}
      </Box>
    </CardLayout>
  );
}

function CompactUsageRow({ card }: { card: UsageCardData }): React.ReactElement {
  const elapsed = card.elapsedMs !== undefined ? ` · ${(card.elapsedMs / 1000).toFixed(1)}s` : "";
  return (
    <Box flexDirection="row" gap={1}>
      <Text color={FG.faint}>{`Σ turn ${card.turn}`}</Text>
      <Text color={FG.meta}>
        {`${compactNum(card.tokens.prompt)} prompt · ${compactNum(card.tokens.output)} out`}
      </Text>
      <Text color={FG.faint}>· cache</Text>
      <Text color={TONE.ok}>{`${(card.cacheHit * 100).toFixed(0)}%`}</Text>
      <Text color={FG.faint}>{`· ${formatCNY(card.cost)}${elapsed}`}</Text>
      {card.balance !== undefined ? (
        <>
          <Text color={FG.faint}>·</Text>
          <Text color={TONE.brand}>{`¥${card.balance.toFixed(2)}`}</Text>
        </>
      ) : null}
    </Box>
  );
}
