import { Box, Text } from "ink";
// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React from "react";
import { t } from "../../../i18n/index.js";
import { CardHeader } from "../primitives/CardHeader.js";
import type { UserCard as UserCardData } from "../state/cards.js";
import { CARD, FG, SURFACE } from "../theme/tokens.js";
import { formatRelativeTime } from "./time.js";

export function UserCard({ card }: { card: UserCardData }): React.ReactElement {
  return (
    <Box flexDirection="row" marginTop={1}>
      <Box width={1} backgroundColor={CARD.user.color} flexShrink={0} />
      <Box flexDirection="column" flexGrow={1} paddingLeft={1} backgroundColor={SURFACE.bgElev}>
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
    </Box>
  );
}
