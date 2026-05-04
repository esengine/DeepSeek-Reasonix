import { Box, Text } from "ink";
// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React from "react";
import { Card } from "../primitives/Card.js";
import { CardHeader } from "../primitives/CardHeader.js";
import type { DiffCard as DiffCardData } from "../state/cards.js";
import { FG, TONE } from "../theme/tokens.js";

const LINE_COLOR = {
  ctx: FG.sub,
  add: TONE.ok,
  del: TONE.err,
  fold: FG.faint,
} as const;

const LINE_GLYPH = {
  ctx: " ",
  add: "+",
  del: "-",
  fold: "⋮",
} as const;

export function DiffCard({ card }: { card: DiffCardData }): React.ReactElement {
  const showFooter = card.hunks.length > 0;
  return (
    <Card tone={TONE.ok}>
      <CardHeader
        glyph="±"
        tone={TONE.ok}
        title={card.file}
        titleColor={FG.body}
        meta={[
          { text: `+${card.stats.add}`, color: TONE.ok },
          { text: `-${card.stats.del}`, color: TONE.err },
        ]}
      />
      {card.hunks.map((hunk) => (
        <Box key={`${card.id}:${hunk.header}`} flexDirection="column">
          <Text italic color={FG.faint}>
            {hunk.header}
          </Text>
          {hunk.lines.map((line, li) => (
            <Box key={`${card.id}:${hunk.header}:${li}`} flexDirection="row" gap={1}>
              <Text color={LINE_COLOR[line.kind]}>{LINE_GLYPH[line.kind]}</Text>
              <Text color={LINE_COLOR[line.kind]} dimColor={line.kind === "ctx"}>
                {line.text}
              </Text>
            </Box>
          ))}
        </Box>
      ))}
      {showFooter && (
        <Box flexDirection="row" gap={2}>
          <Text bold color={TONE.ok}>
            [a] apply
          </Text>
          <Text color={FG.sub}>[s] skip</Text>
          <Text bold color={TONE.err}>
            [r] reject
          </Text>
        </Box>
      )}
    </Card>
  );
}
