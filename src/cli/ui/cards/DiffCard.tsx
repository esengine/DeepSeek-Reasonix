import { Box, Text } from "ink";
// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React from "react";
import type { DiffCard as DiffCardData } from "../state/cards.js";
import { FG, TONE } from "../theme/tokens.js";
import { CardLayout } from "./CardLayout.js";

const LINE_COLOR = {
  ctx: FG.sub,
  add: TONE.ok,
  del: TONE.err,
  fold: FG.faint,
} as const;

export function DiffCard({ card }: { card: DiffCardData }): React.ReactElement {
  const stats = (
    <>
      <Text color={TONE.ok}>{`+${card.stats.add}`}</Text>
      <Text color={FG.faint}>/</Text>
      <Text color={TONE.err}>{`-${card.stats.del}`}</Text>
    </>
  );
  const showFooter = card.hunks.length > 0;
  return (
    <CardLayout glyph="±" tone={TONE.ok} title={card.file} trailing={stats}>
      {card.hunks.map((hunk, hi) => (
        <Box key={`${card.id}:${hunk.header}`} flexDirection="column" marginTop={hi > 0 ? 1 : 0}>
          <Text italic color={FG.faint}>
            {hunk.header}
          </Text>
          {hunk.lines.map((line, li) => (
            <Text key={`${card.id}:${hunk.header}:${li}`} color={LINE_COLOR[line.kind]}>
              {line.text || " "}
            </Text>
          ))}
        </Box>
      ))}
      {showFooter ? (
        <Box flexDirection="row" gap={1} marginTop={1}>
          <Text bold color={TONE.ok}>
            [a] apply
          </Text>
          <Text color={FG.sub}>[s] skip</Text>
          <Text bold color={TONE.err}>
            [r] reject
          </Text>
        </Box>
      ) : null}
    </CardLayout>
  );
}
