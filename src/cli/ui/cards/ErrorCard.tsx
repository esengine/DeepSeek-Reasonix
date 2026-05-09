import { Box, Text } from "ink";
// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React from "react";
import { t } from "../../../i18n/index.js";
import { Card } from "../primitives/Card.js";
import { CardHeader } from "../primitives/CardHeader.js";
import type { ErrorCard as ErrorCardData } from "../state/cards.js";
import { FG, TONE } from "../theme/tokens.js";

const STACK_TAIL = 5;

export function ErrorCard({ card }: { card: ErrorCardData }): React.ReactElement {
  const retryNote =
    card.retries !== undefined && card.retries > 0
      ? `${card.retries} ${t("cardLabels.retries")}`
      : null;
  const stackLines = card.stack ? card.stack.split("\n") : [];
  const stackTrunc = stackLines.length > STACK_TAIL;
  const stackVisible = stackTrunc ? stackLines.slice(-STACK_TAIL) : stackLines;
  const stackHidden = stackTrunc ? stackLines.length - stackVisible.length : 0;
  const hasStack = stackVisible.length > 0;
  const messageLines = card.message.split("\n");

  return (
    <Card tone={TONE.err}>
      <CardHeader
        glyph="✖"
        tone={TONE.err}
        title={card.title || t("cardTitles.error")}
        meta={retryNote ? [retryNote] : undefined}
      />
      {messageLines.map((line, i) => (
        <Text key={`${card.id}:msg:${i}`} color={TONE.err}>
          {line || " "}
        </Text>
      ))}
      {hasStack ? (
        <Box flexDirection="column" marginTop={1}>
          <Text color={FG.meta}>{t("cardLabels.stackTrace")}</Text>
          {stackHidden > 0 ? (
            <Text color={FG.faint}>
              {t(
                stackHidden === 1 ? "cardLabels.earlierStackLine" : "cardLabels.earlierStackLines",
                { count: stackHidden },
              )}
            </Text>
          ) : null}
          {stackVisible.map((line, i) => (
            <Text key={`${card.id}:stk:${stackHidden + i}`} color={FG.meta}>
              {line || " "}
            </Text>
          ))}
        </Box>
      ) : null}
    </Card>
  );
}
