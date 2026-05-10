import { Box, Text } from "ink";
// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React from "react";
import { t } from "../../../i18n/index.js";
import { Card } from "../primitives/Card.js";
import { CardHeader } from "../primitives/CardHeader.js";
import type { MemoryCard as MemoryCardData, MemoryEntry } from "../state/cards.js";
import { FG, TONE } from "../theme/tokens.js";

const CATEGORY_ORDER: ReadonlyArray<MemoryEntry["category"]> = [
  "user",
  "feedback",
  "project",
  "reference",
];

function categoryLabel(c: MemoryEntry["category"]): string {
  switch (c) {
    case "user":
      return t("cardLabels.categoryUser");
    case "feedback":
      return t("cardLabels.categoryFeedback");
    case "project":
      return t("cardLabels.categoryProject");
    case "reference":
      return t("cardLabels.categoryReference");
  }
}

const CATEGORY_GLYPH: Record<MemoryEntry["category"], string> = {
  user: "◇",
  feedback: "✦",
  project: "◇",
  reference: "→",
};

const CATEGORY_GLYPH_COLOR: Record<MemoryEntry["category"], string> = {
  user: FG.meta,
  feedback: TONE.warn,
  project: FG.meta,
  reference: TONE.info,
};

export function MemoryCard({ card }: { card: MemoryCardData }): React.ReactElement {
  const counts = countByCategory(card.entries);
  const summary = CATEGORY_ORDER.filter((c) => counts[c] > 0)
    .map((c) => `${counts[c]} ${categoryLabel(c)}`)
    .join(" · ");
  const tokens =
    card.tokens > 1024
      ? `~${(card.tokens / 1024).toFixed(1)}K ${t("cardLabels.tok")}`
      : `~${card.tokens} ${t("cardLabels.tok")}`;
  return (
    <Card tone={FG.meta}>
      <CardHeader
        glyph="⌑"
        tone={FG.meta}
        title={t("cardTitles.context")}
        titleColor={FG.sub}
        meta={summary ? [summary, tokens] : [tokens]}
      />
      {CATEGORY_ORDER.filter((c) => counts[c] > 0).map((category) => {
        const all = card.entries.filter((e) => e.category === category);
        const shown = all.slice(0, 5);
        const remaining = all.length - shown.length;
        return (
          <Box key={category} flexDirection="column">
            <Text color={FG.faint}>{`${categoryLabel(category)} (${counts[category]})`}</Text>
            {shown.map((entry) => (
              <Box key={`${category}:${entry.summary}`} flexDirection="row" gap={1}>
                <Text color={CATEGORY_GLYPH_COLOR[category]}>{CATEGORY_GLYPH[category]}</Text>
                <Text color={FG.sub}>{entry.summary}</Text>
              </Box>
            ))}
            {remaining > 0 ? (
              <Text color={FG.faint}>{t("cardLabels.more", { count: remaining })}</Text>
            ) : null}
          </Box>
        );
      })}
    </Card>
  );
}

function countByCategory(
  entries: ReadonlyArray<MemoryEntry>,
): Record<MemoryEntry["category"], number> {
  const out: Record<MemoryEntry["category"], number> = {
    user: 0,
    feedback: 0,
    project: 0,
    reference: 0,
  };
  for (const e of entries) out[e.category] += 1;
  return out;
}
