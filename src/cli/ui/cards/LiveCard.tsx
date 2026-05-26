import { Box, Text } from "ink";
// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React from "react";
import { PULSE_CIRCLE, PULSE_DIAMOND, Pulse } from "../primitives/Pulse.js";
import type { LiveCard as LiveCardData } from "../state/cards.js";
import { FG, TONE } from "../theme/tokens.js";

const TONE_TO_COLOR = {
  ok: TONE.ok,
  warn: TONE.warn,
  err: TONE.err,
  info: TONE.info,
  brand: TONE.brand,
  accent: TONE.accent,
  ghost: FG.meta,
} as const;

const VARIANT_GLYPH = {
  thinking: "◆",
  undo: "↶",
  ctxPressure: "⚠",
  aborted: "—",
  retry: "↻",
  checkpoint: "●",
  stepProgress: "✓",
  mcpEvent: "●",
  sessionOp: "●",
} as const;

const PULSING_VARIANTS = new Set<LiveCardData["variant"]>(["thinking", "retry", "sessionOp"]);

export function LiveCard({ card }: { card: LiveCardData }): React.ReactElement {
  const color = TONE_TO_COLOR[card.tone];
  const isPulsing = PULSING_VARIANTS.has(card.variant);
  const frames = card.variant === "thinking" ? PULSE_DIAMOND : PULSE_CIRCLE;
  const settled = VARIANT_GLYPH[card.variant];
  return (
    <Box paddingLeft={2} flexDirection="row" gap={1}>
      {isPulsing ? (
        <Pulse active frames={frames} settled={settled} color={color} />
      ) : (
        <Text bold color={color}>
          {VARIANT_GLYPH[card.variant]}
        </Text>
      )}
      <Text color={FG.body}>{card.text}</Text>
      {card.meta !== undefined ? <Text color={FG.faint}>{`· ${card.meta}`}</Text> : null}
    </Box>
  );
}
