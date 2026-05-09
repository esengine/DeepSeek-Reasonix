import { Box, Text } from "ink";
// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React from "react";
import { t } from "../../../i18n/index.js";
import { Card } from "../primitives/Card.js";
import { CardHeader } from "../primitives/CardHeader.js";
import type { SearchCard as SearchCardData, SearchHit } from "../state/cards.js";
import { FG, TONE } from "../theme/tokens.js";

export function SearchCard({ card }: { card: SearchCardData }): React.ReactElement {
  const fileCount = new Set(card.hits.map((h) => h.file)).size;
  const elapsed = `${(card.elapsedMs / 1000).toFixed(2)}s`;
  const stats = t(card.hits.length === 1 ? "cardLabels.hitSingular" : "cardLabels.hitsPlural", {
    count: card.hits.length,
    files: fileCount,
  });

  const grouped = groupByFile(card.hits.slice(0, 10));

  return (
    <Card tone={TONE.info}>
      <CardHeader
        glyph="⊙"
        tone={TONE.info}
        title={t("cardTitles.search")}
        subtitle={`"${card.query}"`}
        meta={[stats, elapsed]}
      />
      {grouped.map(([file, hits]) => (
        <Box key={file} flexDirection="column">
          <Text bold color={FG.strong}>
            {file}
          </Text>
          {hits.map((h, i) => (
            <Box key={`${file}:${h.line}:${i}`} flexDirection="row" gap={1}>
              <Text color={FG.faint}>{`${h.line.toString().padStart(4)} │`}</Text>
              <HighlightedLine text={h.preview} start={h.matchStart} end={h.matchEnd} />
            </Box>
          ))}
        </Box>
      ))}
      {card.hits.length > 10 ? (
        <Text color={FG.faint}>
          {t(
            card.hits.length - 10 === 1
              ? "cardLabels.moreHitSingular"
              : "cardLabels.moreHitsPlural",
            { count: card.hits.length - 10 },
          )}
        </Text>
      ) : null}
    </Card>
  );
}

function HighlightedLine({
  text,
  start,
  end,
}: {
  text: string;
  start: number;
  end: number;
}): React.ReactElement {
  if (start < 0 || end <= start || end > text.length) {
    return <Text color={FG.sub}>{text}</Text>;
  }
  return (
    <>
      <Text color={FG.sub}>{text.slice(0, start)}</Text>
      <Text bold inverse>
        {text.slice(start, end)}
      </Text>
      <Text color={FG.sub}>{text.slice(end)}</Text>
    </>
  );
}

function groupByFile(hits: ReadonlyArray<SearchHit>): Array<[string, SearchHit[]]> {
  const map = new Map<string, SearchHit[]>();
  for (const h of hits) {
    const list = map.get(h.file) ?? [];
    list.push(h);
    map.set(h.file, list);
  }
  return Array.from(map.entries());
}
