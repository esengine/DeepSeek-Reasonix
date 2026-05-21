import { Box, Text, useStdout } from "ink";
// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for fragments
import React from "react";
import stringWidth from "string-width";
import { t } from "../../../i18n/index.js";
import { useChatScrollState } from "../state/chat-scroll-provider.js";
import { FG } from "../theme/tokens.js";

/**
 * Renders either the input area (pinned) or the "reading history" hint
 * (scrolled up). Reads `pinned` from the chat-scroll store directly so
 * AppInner doesn't subscribe; toggling pinned only re-renders this leaf.
 */
export function InputAreaWithHistoryHint({
  inputArea,
}: {
  inputArea: React.ReactNode;
}): React.ReactElement {
  const pinned = useChatScrollState((s) => s.pinned);
  const { stdout } = useStdout();
  if (!pinned) {
    const text = t("app.historyScrollHint");
    const cols = stdout?.columns ?? 80;
    const pad = Math.max(0, cols - stringWidth(text));
    return (
      <Box flexDirection="row">
        <Box width={1} backgroundColor="#0153e5" />
        <Box flexGrow={1} backgroundColor="#1e1e1e">
          <Text color={FG.faint}>{text + " ".repeat(pad)}</Text>
        </Box>
      </Box>
    );
  }
  return <>{inputArea}</>;
}
