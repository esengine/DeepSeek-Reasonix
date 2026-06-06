import { Box, Text } from "ink";
// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React from "react";
import { Card } from "../primitives/Card.js";
import { CardHeader } from "../primitives/CardHeader.js";
import type { CompactionCard as CompactionCardData } from "../state/cards.js";
import { FG, TONE } from "../theme/tokens.js";

const PREVIEW_LINES = 3;

export function CompactionCard({ card }: { card: CompactionCardData }): React.ReactElement {
  const lines = card.summary.split("\n");
  const previewLines = lines.slice(0, PREVIEW_LINES);
  const hiddenCount = Math.max(0, lines.length - PREVIEW_LINES);
  return (
    <Card tone={TONE.info}>
      <CardHeader
        glyph="≡"
        tone={TONE.info}
        title="compacted history"
        meta={[`${card.summary.length.toLocaleString()} chars · ${lines.length} lines`]}
      />
      {previewLines.map((line, i) => (
        <Text key={`${card.id}:p:${i}`} color={FG.sub}>
          {line || " "}
        </Text>
      ))}
      {hiddenCount > 0 ? (
        <Box marginTop={1}>
          <Text color={FG.sub} dim>
            … {hiddenCount} more line{hiddenCount === 1 ? "" : "s"} (full summary in session log)
          </Text>
        </Box>
      ) : null}
    </Card>
  );
}
