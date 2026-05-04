import { Box, Text } from "ink";
// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React from "react";
import type { SearchCard as SearchCardData, SearchHit } from "../state/cards.js";
import { FG, TONE } from "../theme/tokens.js";
import { CardLayout } from "./CardLayout.js";

export function SearchCard({ card }: { card: SearchCardData }): React.ReactElement {
  const fileCount = new Set(card.hits.map((h) => h.file)).size;
  const meta = `${card.hits.length} hit${card.hits.length === 1 ? "" : "s"} in ${fileCount} file${
    fileCount === 1 ? "" : "s"
  } · ${(card.elapsedMs / 1000).toFixed(2)}s`;

  return (
    <CardLayout glyph="⊙" tone={TONE.info} title={`"${card.query}"`} meta={meta}>
      {card.hits.length === 0 ? null : (
        <>
          {groupByFile(card.hits.slice(0, 10)).map(([file, hits], gi) => (
            <Box key={file} flexDirection="column" marginTop={gi > 0 ? 1 : 0}>
              <Text bold color={FG.strong}>
                {file}
              </Text>
              {hits.map((h, i) => (
                <Box key={`${file}:${h.line}:${i}`} flexDirection="row">
                  <Text color={FG.faint}>{`${h.line.toString().padStart(4)} │ `}</Text>
                  <HighlightedLine text={h.preview} start={h.matchStart} end={h.matchEnd} />
                </Box>
              ))}
            </Box>
          ))}
          {card.hits.length > 10 ? (
            <Text color={FG.faint}>{`⋮ +${card.hits.length - 10} more hits`}</Text>
          ) : null}
        </>
      )}
    </CardLayout>
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
