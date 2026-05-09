import { Box, Text } from "ink";
// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React from "react";
import stringWidth from "string-width";
import { t } from "../../../i18n/index.js";
import type { TipCard as TipCardData, TipRow as TipRowData } from "../state/cards.js";
import { FG, TONE } from "../theme/tokens.js";

const KEY_GUTTER = 4;

export function TipCard({ card }: { card: TipCardData }): React.ReactElement {
  const keyWidth = card.sections.reduce(
    (max, sec) => sec.rows.reduce((m, r) => Math.max(m, stringWidth(r.key)), max),
    0,
  );
  return (
    <Box flexDirection="column" paddingLeft={2} marginY={1}>
      <Box flexDirection="row" justifyContent="space-between">
        <Box flexDirection="row" gap={1}>
          <Text color={TONE.accent} bold>
            ⓘ
          </Text>
          <Text color={FG.body} bold>
            {card.topic}
          </Text>
        </Box>
        {card.oneTime ? <Text color={FG.faint}>{t("ui.tipShownOnce")}</Text> : null}
      </Box>
      {card.sections.map((section, i) => (
        <Box key={section.title ?? `section-${i}`} flexDirection="column" marginTop={1}>
          {section.title ? (
            <Box marginBottom={0}>
              <Text color={FG.sub}>{section.title}</Text>
            </Box>
          ) : null}
          {section.rows.map((row) => (
            <TipRowRender
              key={row.key}
              row={row}
              keyWidth={keyWidth}
              indent={section.title ? 2 : 0}
            />
          ))}
        </Box>
      ))}
      {card.footer ? (
        <Box marginTop={1}>
          <Text color={FG.faint}>{card.footer}</Text>
        </Box>
      ) : null}
    </Box>
  );
}

function TipRowRender({
  row,
  keyWidth,
  indent,
}: {
  row: TipRowData;
  keyWidth: number;
  indent: number;
}) {
  const pad = " ".repeat(Math.max(0, keyWidth - stringWidth(row.key) + KEY_GUTTER));
  const lead = indent > 0 ? " ".repeat(indent) : "";
  return (
    <Box flexDirection="row">
      {lead ? <Text>{lead}</Text> : null}
      <Text color={TONE.accent}>{row.key}</Text>
      <Text>{pad}</Text>
      <Text color={FG.body}>{row.text}</Text>
    </Box>
  );
}
