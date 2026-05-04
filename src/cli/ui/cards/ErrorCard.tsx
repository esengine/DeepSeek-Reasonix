import { Box, Text } from "ink";
// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React from "react";
import type { ErrorCard as ErrorCardData } from "../state/cards.js";
import { FG, TONE } from "../theme/tokens.js";
import { CardLayout } from "./CardLayout.js";

const STACK_TAIL = 5;

export function ErrorCard({ card }: { card: ErrorCardData }): React.ReactElement {
  const retryNote =
    card.retries !== undefined && card.retries > 0
      ? `${card.retries} retr${card.retries === 1 ? "y" : "ies"}`
      : undefined;
  const stackLines = card.stack ? card.stack.split("\n") : [];
  const stackTrunc = stackLines.length > STACK_TAIL;
  const stackVisible = stackTrunc ? stackLines.slice(-STACK_TAIL) : stackLines;
  const stackHidden = stackTrunc ? stackLines.length - stackVisible.length : 0;
  const hasStack = stackVisible.length > 0;
  const messageLines = card.message.split("\n");

  return (
    <CardLayout glyph="✖" tone={TONE.err} title={card.title || "error"} meta={retryNote}>
      {messageLines.map((line, i) => (
        <Text key={`${card.id}:msg:${i}`} color={TONE.err}>
          {line || " "}
        </Text>
      ))}
      {hasStack ? (
        <>
          <Box marginTop={1}>
            <Text color={FG.meta}>stack trace</Text>
          </Box>
          {stackHidden > 0 ? (
            <Text color={FG.faint}>
              {`⋮ ${stackHidden} earlier stack line${stackHidden === 1 ? "" : "s"} hidden`}
            </Text>
          ) : null}
          {stackVisible.map((line, i) => (
            <Text key={`${card.id}:stk:${stackHidden + i}`} color={FG.meta}>
              {line || " "}
            </Text>
          ))}
        </>
      ) : null}
    </CardLayout>
  );
}
