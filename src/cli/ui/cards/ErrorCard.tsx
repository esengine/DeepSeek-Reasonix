import { Box, Text } from "ink";
// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React from "react";
import type { ErrorCard as ErrorCardData } from "../state/cards.js";
import { FG, TONE } from "../theme/tokens.js";

const STACK_TAIL = 5;

export function ErrorCard({ card }: { card: ErrorCardData }): React.ReactElement {
  const retryNote =
    card.retries !== undefined && card.retries > 0
      ? `· ${card.retries} retr${card.retries === 1 ? "y" : "ies"}`
      : null;
  const stackLines = card.stack ? card.stack.split("\n") : [];
  const stackTrunc = stackLines.length > STACK_TAIL;
  const stackVisible = stackTrunc ? stackLines.slice(-STACK_TAIL) : stackLines;
  const stackHidden = stackTrunc ? stackLines.length - stackVisible.length : 0;
  const hasStack = stackVisible.length > 0;
  const messageLines = card.message.split("\n");

  return (
    <Box flexDirection="column" marginTop={1}>
      <Box flexDirection="row" gap={1}>
        <Text color={TONE.err}>✖</Text>
        <Text color={TONE.err} bold>
          {card.title || "error"}
        </Text>
        {retryNote ? <Text color={FG.faint}>{retryNote}</Text> : null}
      </Box>
      {messageLines.map((line, i) => (
        <Box key={`${card.id}:msg:${i}`} paddingLeft={2}>
          <Text color={TONE.err}>{line || " "}</Text>
        </Box>
      ))}
      {hasStack ? (
        <>
          <Box paddingLeft={2} marginTop={1}>
            <Text color={FG.meta}>stack trace</Text>
          </Box>
          {stackHidden > 0 ? (
            <Box paddingLeft={2}>
              <Text color={FG.faint}>
                {`⋮ ${stackHidden} earlier stack line${stackHidden === 1 ? "" : "s"} hidden`}
              </Text>
            </Box>
          ) : null}
          {stackVisible.map((line, i) => (
            <Box key={`${card.id}:stk:${stackHidden + i}`} paddingLeft={2}>
              <Text color={FG.meta}>{line || " "}</Text>
            </Box>
          ))}
        </>
      ) : null}
    </Box>
  );
}
