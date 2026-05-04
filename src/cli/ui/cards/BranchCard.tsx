import { Box, Text } from "ink";
// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React from "react";
import type { BranchCard as BranchCardData } from "../state/cards.js";
import { FG, TONE } from "../theme/tokens.js";
import { CardLayout } from "./CardLayout.js";
import { ProgressBar } from "./ProgressBar.js";

export function BranchCard({ card }: { card: BranchCardData }): React.ReactElement {
  const ratio = card.total > 0 ? card.completed / card.total : 0;
  const tone = card.done ? TONE.ok : TONE.brand;
  return (
    <CardLayout
      glyph="⎇"
      tone={tone}
      title={card.done ? "branching done" : "branching"}
      meta={`${card.completed}/${card.total} samples`}
    >
      <Box flexDirection="row" gap={1}>
        <ProgressBar ratio={ratio} color={tone} />
        <Text color={FG.faint}>{`${(ratio * 100).toFixed(0)}%`}</Text>
      </Box>
      {!card.done && card.completed > 0 ? (
        <Text color={FG.faint}>
          {`latest: #${card.latestIndex} · T=${card.latestTemperature.toFixed(2)} · ${card.latestUncertainties} unc`}
        </Text>
      ) : null}
    </CardLayout>
  );
}
