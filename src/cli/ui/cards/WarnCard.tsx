import { Box, Text } from "ink";
// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React from "react";
import type { WarnCard as WarnCardData } from "../state/cards.js";
import { FG, TONE } from "../theme/tokens.js";

export function WarnCard({ card }: { card: WarnCardData }): React.ReactElement {
  const messageLines = card.message.length > 0 ? card.message.split("\n") : [];
  return (
    <Box flexDirection="column" marginTop={1}>
      <Box flexDirection="row" gap={1}>
        <Text color={TONE.warn}>⚠</Text>
        <Text color={TONE.warn} bold>
          {card.title}
        </Text>
        {card.detail ? <Text color={FG.faint}>{`· ${card.detail}`}</Text> : null}
      </Box>
      {messageLines.map((line, i) => (
        <Box key={`${card.id}:${i}`} paddingLeft={2}>
          <Text color={FG.body}>{line || " "}</Text>
        </Box>
      ))}
    </Box>
  );
}
