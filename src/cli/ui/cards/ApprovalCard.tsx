import { Box, Text, useStdout } from "ink";
// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React from "react";
import { t } from "../../../i18n/index.js";
import { CARD, type CardTone, FG, SURFACE } from "../theme/tokens.js";

const SEPARATOR_PAD = 6;
const MIN_SEPARATOR = 20;

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
  warn: { color: CARD.warn.color, glyph: "?" },
  error: { color: CARD.error.color, glyph: "✗" },
  approval: { color: CARD.approval.color, glyph: "?" },
  diff: { color: CARD.diff.color, glyph: "±" },
  memory: { color: CARD.memory.color, glyph: "⌑" },
  user: { color: CARD.user.color, glyph: "◇" },
  ok: { color: CARD.diff.color, glyph: "✓" },
  accent: { color: CARD.plan.color, glyph: "⊞" },
  info: { color: CARD.tool.color, glyph: "?" },
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
  const { stdout } = useStdout();
  const cols = stdout?.columns ?? 80;
  const ruleWidth = Math.max(MIN_SEPARATOR, cols - SEPARATOR_PAD);

  return (
    <Box flexDirection="column" marginY={1}>
      <Box flexDirection="row">
        <Text color={palette.color} backgroundColor={SURFACE.bgElev}>
          {" ▎ "}
        </Text>
        <Text bold color={palette.color} backgroundColor={SURFACE.bgElev}>
          {`${headerGlyph}  `}
        </Text>
        <Text bold color={FG.strong} backgroundColor={SURFACE.bgElev}>
          {` ${title} `}
        </Text>
        {metaRight !== undefined && (
          <Text color={metaRightColor ?? FG.faint} backgroundColor={SURFACE.bgElev}>
            {`  ${metaRight} `}
          </Text>
        )}
      </Box>
      <Box flexDirection="column" paddingX={2} marginTop={1}>
        {children}
      </Box>
      <Box paddingX={2} marginTop={1}>
        <Text color={FG.faint}>{"─".repeat(ruleWidth)}</Text>
      </Box>
      <Box paddingX={2}>
        <Text color={FG.faint}>{effectiveFooter}</Text>
      </Box>
    </Box>
  );
}
