import { Box, Text } from "ink";
// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React from "react";
import { t } from "../../../i18n/index.js";
import { CARD, type CardTone, FG } from "../theme/tokens.js";

export interface ApprovalCardProps {
  tone:
    | Extract<CardTone, "warn" | "error" | "approval" | "diff" | "memory" | "user">
    | "ok"
    | "accent"
    | "info";
  glyph?: string;
  title: string;
  metaRight?: string;
  /** Override metaRight color — defaults to FG.faint. Use the tone color to match design's status indicator (e.g. "awaiting" in accent for plan-confirm). */
  metaRightColor?: string;
  children?: React.ReactNode;
  footerHint?: string;
}

const TONE_PALETTE = {
  warn: { color: CARD.warn.color, glyph: "⚠" },
  error: { color: CARD.error.color, glyph: "✗" },
  approval: { color: CARD.approval.color, glyph: "●" },
  diff: { color: CARD.diff.color, glyph: "±" },
  memory: { color: CARD.memory.color, glyph: "●" },
  user: { color: CARD.user.color, glyph: "●" },
  ok: { color: CARD.diff.color, glyph: "✓" },
  accent: { color: CARD.plan.color, glyph: "●" },
  info: { color: CARD.tool.color, glyph: "●" },
} as const;

export function ApprovalCard({
  tone,
  glyph,
  title,
  metaRight,
  metaRightColor,
  children,
  footerHint,
}: ApprovalCardProps): React.ReactElement {
  const effectiveFooter = footerHint ?? t("cardLabels.defaultFooter");
  const palette = TONE_PALETTE[tone];
  const headerGlyph = glyph ?? palette.glyph;

  return (
    <Box flexDirection="column" marginY={1} flexShrink={0}>
      <Box
        flexDirection="column"
        borderStyle="round"
        borderLeft={false}
        borderRight={false}
        borderColor={palette.color}
        paddingX={1}
        flexShrink={0}
      >
        <Box flexDirection="row" gap={1}>
          <Text bold color={palette.color}>
            {headerGlyph}
          </Text>
          <Text bold color={FG.strong}>
            {title}
          </Text>
          {metaRight !== undefined && <Text color={metaRightColor ?? FG.faint}>{metaRight}</Text>}
        </Box>
        <Box flexDirection="column" marginTop={1} flexShrink={0}>
          {children}
        </Box>
      </Box>
      <Box paddingX={2} marginTop={1} flexShrink={0}>
        <Text color={FG.faint}>{effectiveFooter}</Text>
      </Box>
    </Box>
  );
}
