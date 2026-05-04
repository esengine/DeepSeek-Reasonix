import { Text } from "ink";
// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React from "react";
import type { WarnCard as WarnCardData } from "../state/cards.js";
import { FG, TONE } from "../theme/tokens.js";
import { CardLayout } from "./CardLayout.js";

export function WarnCard({ card }: { card: WarnCardData }): React.ReactElement {
  const messageLines = card.message.length > 0 ? card.message.split("\n") : [];
  return (
    <CardLayout glyph="⚠" tone={TONE.warn} title={card.title} meta={card.detail}>
      {messageLines.map((line, i) => (
        <Text key={`${card.id}:${i}`} color={FG.body}>
          {line || " "}
        </Text>
      ))}
    </CardLayout>
  );
}
