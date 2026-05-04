import { Box, Text } from "ink";
// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React from "react";
import { Spinner } from "../primitives/Spinner.js";
import type { LiveCard as LiveCardData } from "../state/cards.js";
import { FG, TONE } from "../theme/tokens.js";

/** LiveCard is a single transient row — used for thinking / undo / aborted /
 *  retry hints. It deliberately does NOT use CardLayout: no marginTop spacer,
 *  no body, no bold-title pattern. The signal is "something is happening
 *  right now"; full card chrome would be too heavy. */

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
  thinking: null,
  undo: "↶",
  ctxPressure: "⚠",
  aborted: "—",
  retry: "↻",
  checkpoint: "⛁",
  stepProgress: "✓",
  mcpEvent: "⌘",
  sessionOp: "◍",
} as const;

export function LiveCard({ card }: { card: LiveCardData }): React.ReactElement {
  const color = TONE_TO_COLOR[card.tone];
  const glyph = VARIANT_GLYPH[card.variant];
  return (
    <Box flexDirection="row" gap={1}>
      {card.variant === "thinking" ? (
        <Spinner kind="circle" color={color} bold />
      ) : (
        <Text bold color={color}>
          {glyph}
        </Text>
      )}
      <Text color={FG.body}>{card.text}</Text>
      {card.meta !== undefined ? <Text color={FG.faint}>{card.meta}</Text> : null}
    </Box>
  );
}
