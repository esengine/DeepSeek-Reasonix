import { Box, Text, useAnimationFrame } from "ink";
// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React from "react";

const DEFAULT_FRAME_MS = 140;

export interface PulseProps {
  frames: readonly string[];
  /** Glyph when inactive. Defaults to the last (most-filled) frame. */
  settled?: string;
  active: boolean;
  color?: string;
  frameMs?: number;
}

export function Pulse({
  frames,
  settled,
  active,
  color,
  frameMs = DEFAULT_FRAME_MS,
}: PulseProps): React.ReactElement {
  // keepAlive=true: useAnimationFrame drives the shared Ink clock. Pass null
  // when inactive so the clock can shut down if no other animations need it.
  const [ref, time] = useAnimationFrame(active ? frameMs : null);
  const settledGlyph = settled ?? frames[frames.length - 1] ?? "";
  if (!active)
    return (
      <Box ref={ref}>
        <Text color={color}>{settledGlyph}</Text>
      </Box>
    );
  const idx = Math.floor(time / frameMs) % frames.length;
  return (
    <Box ref={ref}>
      <Text color={color}>{frames[idx] ?? settledGlyph}</Text>
    </Box>
  );
}

export const PULSE_DIAMOND = ["◇", "◈", "◆", "◈"] as const;
export const PULSE_SQUARE = ["▢", "▣", "▤", "▣"] as const;
export const PULSE_TRIANGLE = ["▷", "▶", "▷", "▶"] as const;
export const PULSE_CIRCLE = ["◌", "◐", "◑", "◒", "◓", "●"] as const;
export const PULSE_HEX = ["⬡", "⬢", "⬡", "⬢"] as const;
