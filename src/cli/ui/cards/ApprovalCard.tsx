import { Box, Text, useStdout } from "ink";
// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React from "react";
import { CARD, type CardTone, FG } from "../theme/tokens.js";
import { CardLayout } from "./CardLayout.js";

/** Generic confirmation-modal wrapper used by Plan / Edit / Choice / Shell /
 *  Checkpoint / Revise pickers. Looks like a regular card on top with a
 *  horizontal rule + footer hint underneath; children fill the body. */

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
  /** Override metaRight color — defaults to FG.faint. Use the tone color to
   *  match design's status indicator (e.g. "awaiting" in accent for plan-confirm). */
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

const DEFAULT_FOOTER = "↑↓ pick  ·  ⏎ confirm  ·  esc cancel";

export function ApprovalCard({
  tone,
  glyph,
  title,
  metaRight,
  metaRightColor,
  children,
  footerHint = DEFAULT_FOOTER,
}: ApprovalCardProps): React.ReactElement {
  const palette = TONE_PALETTE[tone];
  const headerGlyph = glyph ?? palette.glyph;
  const { stdout } = useStdout();
  const cols = stdout?.columns ?? 80;
  const ruleWidth = Math.max(MIN_SEPARATOR, cols - SEPARATOR_PAD);
  const meta = metaRight ? <Text color={metaRightColor ?? FG.faint}>{metaRight}</Text> : undefined;

  return (
    <CardLayout glyph={headerGlyph} tone={palette.color} title={title} meta={meta}>
      {children}
      <Box marginTop={1}>
        <Text color={FG.faint}>{"─".repeat(ruleWidth)}</Text>
      </Box>
      <Text color={FG.faint}>{footerHint}</Text>
    </CardLayout>
  );
}
