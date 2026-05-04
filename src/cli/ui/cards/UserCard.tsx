// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React from "react";
import { Markdown } from "../markdown.js";
import type { UserCard as UserCardData } from "../state/cards.js";
import { TONE } from "../theme/tokens.js";
import { CardLayout } from "./CardLayout.js";
import { formatRelativeTime } from "./time.js";

export function UserCard({ card }: { card: UserCardData }): React.ReactElement {
  return (
    <CardLayout glyph="›" tone={TONE.accent} title="you" meta={formatRelativeTime(card.ts)}>
      <Markdown text={card.text} />
    </CardLayout>
  );
}
