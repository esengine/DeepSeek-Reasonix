import { Box, Text } from "ink";
// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React from "react";
import { t } from "../../../i18n/index.js";
import { CardHeader } from "../primitives/CardHeader.js";
import type { UserCard as UserCardData } from "../state/cards.js";
import { CARD, FG, MESSAGE_BG } from "../theme/tokens.js";
import { formatRelativeTime } from "./time.js";

export function UserCard({ card }: { card: UserCardData }): React.ReactElement {
  return (
    <Box flexDirection="column" width="100%" paddingX={1} backgroundColor={MESSAGE_BG.user}>
      <CardHeader
        glyph={CARD.user.glyph}
        tone={CARD.user.color}
        title={t("cardTitles.you")}
        meta={[formatRelativeTime(card.ts)]}
      />
      <Box flexDirection="row" gap={1}>
        <Text color={FG.sub}>↳</Text>
        <Text>{card.text}</Text>
      </Box>
    </Box>
  );
}
