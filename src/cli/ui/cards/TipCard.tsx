import { Box, Text } from "ink";
// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React from "react";
import stringWidth from "string-width";
import { t } from "../../../i18n/index.js";
import type { TipCard as TipCardData } from "../state/cards.js";
import { FG, TONE } from "../theme/tokens.js";

const KEY_GUTTER = 4;

export function TipCard({ card }: { card: TipCardData }): React.ReactElement {
  const keyWidth = card.rows.reduce((max, r) => Math.max(max, stringWidth(r.key)), 0);
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
      {card.rows.length > 0 ? (
        <Box flexDirection="column" marginTop={1}>
          {card.rows.map((row) => (
            <TipRow key={row.key} keyText={row.key} text={row.text} keyWidth={keyWidth} />
          ))}
        </Box>
      ) : null}
      {card.footer ? (
        <Box marginTop={1}>
          <Text color={FG.faint}>{card.footer}</Text>
        </Box>
      ) : null}
    </Box>
  );
}

function TipRow({
  keyText,
  text,
  keyWidth,
}: {
  keyText: string;
  text: string;
  keyWidth: number;
}) {
  const pad = " ".repeat(Math.max(0, keyWidth - stringWidth(keyText) + KEY_GUTTER));
  return (
    <Box flexDirection="row">
      <Text color={TONE.accent}>{keyText}</Text>
      <Text>{pad}</Text>
      <Text color={FG.body}>{text}</Text>
    </Box>
  );
}
