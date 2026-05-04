import { Box, Text } from "ink";
// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React from "react";
import type { CtxCard as CtxCardData } from "../state/cards.js";
import { FG, TONE } from "../theme/tokens.js";
import { CardLayout } from "./CardLayout.js";
import { ProgressBar } from "./ProgressBar.js";

const BAR_CELLS = 32;

function Row({
  label,
  tokens,
  ratio,
  color,
}: {
  label: string;
  tokens: number;
  ratio: number;
  color: string;
}): React.ReactElement {
  return (
    <Box flexDirection="row" gap={1}>
      <Text color={FG.sub}>{label.padEnd(8)}</Text>
      <ProgressBar ratio={ratio} color={color} cells={BAR_CELLS} />
      <Text bold color={FG.body}>
        {tokens.toLocaleString()}
      </Text>
      <Text color={FG.faint}>{`${(ratio * 100).toFixed(1)}%`}</Text>
    </Box>
  );
}

export function CtxCard({ card }: { card: CtxCardData }): React.ReactElement {
  const cap = Math.max(1, card.ctxMax);
  const used = card.systemTokens + card.toolsTokens + card.logTokens + card.inputTokens;
  const usedPct = (used / cap) * 100;
  const meta = `${used.toLocaleString()} / ${cap.toLocaleString()} (${usedPct.toFixed(1)}%)`;

  return (
    <CardLayout glyph="⌘" tone={TONE.brand} title="context window" meta={meta}>
      <Row
        label="system"
        tokens={card.systemTokens}
        ratio={card.systemTokens / cap}
        color={TONE.brand}
      />
      <Row
        label="tools"
        tokens={card.toolsTokens}
        ratio={card.toolsTokens / cap}
        color={TONE.warn}
      />
      <Row label="log" tokens={card.logTokens} ratio={card.logTokens / cap} color={TONE.ok} />
      <Row
        label="input"
        tokens={card.inputTokens}
        ratio={card.inputTokens / cap}
        color={TONE.accent}
      />
      {card.topTools.length > 0 ? (
        <Box flexDirection="column" marginTop={1}>
          <Text color={FG.faint}>
            {`top tools (${card.toolsCount} total · ${card.logMessages} log msgs):`}
          </Text>
          {card.topTools.slice(0, 5).map((t) => (
            <Box key={`${t.turn}-${t.name}`} flexDirection="row" gap={1}>
              <Text color={FG.sub}>{t.name}</Text>
              <Text color={FG.faint}>{`· turn ${t.turn} · ${t.tokens.toLocaleString()}`}</Text>
            </Box>
          ))}
        </Box>
      ) : null}
    </CardLayout>
  );
}
