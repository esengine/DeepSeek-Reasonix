import { Text } from "ink";
// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for fragments
import React from "react";
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
  if (!pinned) {
    return <Text color={FG.faint}>{t("app.historyScrollHint")}</Text>;
  }
  return <>{inputArea}</>;
}
