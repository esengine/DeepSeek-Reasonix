import type { ResolvedBuddyConfig } from "@/config.js";
import { Box, type Color, Text, useAnimationFrame, useStdout } from "ink";
import React from "react";
import { graphemes, padToCells } from "../text-width.js";
import { FG, TONE } from "../theme/tokens.js";
import { type BuddyMood, type BuddyPulse, applyBuddyPulseToMood, buddyPhrase } from "./state.js";

export interface BuddySpriteProps {
  config: ResolvedBuddyConfig;
  mood: BuddyMood;
  pulse?: BuddyPulse | null;
}

const FRAME_MS = 140;
const WAKE = "*";
const PET_MARK = "<3";
const WHALE_WIDTH = 33;
const COMPACT_WIDTH = 27;
const FACE_GLYPHS = "><_•";
const OUTLINE_GLYPHS = ".-'`~/\\_|>()";
const WAVE_FRONT = ["~", "^", "~", "~", "^", "~"] as const;

const WHALE_LINES = [
  "        .--~~~~--.",
  "      .'          `.___/|",
  "     |   •̀ _ •́  >>  ___ >",
  "      \\           __/  \\|",
  "       `-.____.-'",
] as const;

const COMPACT_WHALE_LINES = [
  "    .--~~~--.",
  "  .'        `.___/|",
  " |  •̀ _ •́ >> __ >",
  "  \\       __/ \\|",
  "   `-.__.-'",
] as const;

function moodColor(mood: BuddyMood): Color {
  if (mood === "pet") return TONE.accent;
  if (mood === "thinking") return TONE.brand;
  if (mood === "working") return TONE.info;
  if (mood === "warning") return TONE.warn;
  return FG.meta;
}

function glyphColor(ch: string): Color | undefined {
  if (ch.includes("•") || FACE_GLYPHS.includes(ch)) return FG.strong;
  if (OUTLINE_GLYPHS.includes(ch)) return TONE.info;
  return undefined;
}

function renderOutlineLine(line: string, key: string): React.ReactElement {
  const padded = padToCells(line, WHALE_WIDTH);
  const nodes: React.ReactNode[] = [];
  let buffer = "";
  let currentColor: Color | undefined;
  for (const ch of graphemes(padded)) {
    const color = glyphColor(ch);
    if (color !== currentColor && buffer.length > 0) {
      nodes.push(
        <Text key={`${key}-${nodes.length}`} color={currentColor}>
          {buffer}
        </Text>,
      );
      buffer = "";
    }
    currentColor = color;
    buffer += ch;
  }
  if (buffer.length > 0) {
    nodes.push(
      <Text key={`${key}-${nodes.length}`} color={currentColor}>
        {buffer}
      </Text>,
    );
  }
  return (
    <Box key={key} height={1}>
      {nodes}
    </Box>
  );
}

function blank(width: number): string[] {
  return Array.from({ length: width }, () => " ");
}

function writeAt(row: string[], x: number, text: string): void {
  for (let i = 0; i < text.length; i++) {
    const pos = x + i;
    if (pos >= 0 && pos < row.length) row[pos] = text[i] ?? " ";
  }
}

function spoutRows(frame: number, width: number, mood: BuddyMood, compact: boolean): string[] {
  const rows = [blank(width), blank(width), blank(width)];
  const center = compact ? 9 : 10;
  const sway = [0, 1, 0, -1][Math.floor(frame / 2) % 4] ?? 0;

  if (mood === "pet") {
    writeAt(rows[0]!, center - 3 + sway, PET_MARK);
    writeAt(rows[0]!, center + 3 - sway, PET_MARK);
    writeAt(rows[1]!, center + sway, "o");
    writeAt(rows[2]!, center, "|");
    return rows.map((row) => row.join(""));
  }

  if (mood === "warning") {
    writeAt(rows[0]!, center + sway, "!");
    writeAt(rows[1]!, center, "!");
    writeAt(rows[2]!, center, "|");
    return rows.map((row) => row.join(""));
  }

  writeAt(rows[0]!, center - 2 + sway, ". o");
  writeAt(rows[1]!, center + 1 - sway, "o");
  writeAt(rows[2]!, center + 1, "|");
  if (mood === "working" || mood === "thinking") writeAt(rows[1]!, center + 4 - sway, "o");
  return rows.map((row) => row.join(""));
}

function shiftedWave(
  pattern: readonly string[],
  width: number,
  frame: number,
  speed: number,
): string {
  let out = "";
  const offset = Math.floor(frame / speed) % pattern.length;
  for (let i = 0; i < width; i++) out += pattern[(i + offset) % pattern.length] ?? "~";
  return out;
}

function waveLine(width: number, pad: number, wave: string): string {
  return padToCells(`${" ".repeat(pad)}${wave}`, width);
}

function whaleBob(frame: number): boolean {
  return ([false, false, true, true, false, false] as const)[Math.floor(frame / 3) % 6] ?? false;
}

function AnimatedWhaleScene({
  mood,
  pulse,
  compact,
}: {
  mood: BuddyMood;
  pulse?: BuddyPulse | null;
  compact: boolean;
}): React.ReactElement {
  const [, time] = useAnimationFrame(FRAME_MS);
  const frame = Math.floor(time / FRAME_MS);
  const width = compact ? COMPACT_WIDTH : WHALE_WIDTH;
  const art = compact ? COMPACT_WHALE_LINES : WHALE_LINES;
  const bobDown = whaleBob(frame);
  const currentMood = applyBuddyPulseToMood(mood, pulse);
  const spoutColor =
    currentMood === "pet" ? TONE.accent : currentMood === "warning" ? TONE.warn : TONE.info;
  const spout = spoutRows(frame, width, currentMood, compact);
  const wave = compact ? "~^~~^" : "~^~~^~";
  const wavePad = compact ? 5 : 10;

  return (
    <Box flexDirection="column" alignItems="flex-end" width={width}>
      <Text color={spoutColor}>{spout[0]}</Text>
      <Text color={spoutColor}>{spout[1]}</Text>
      <Text color={spoutColor}>{spout[2]}</Text>
      {bobDown ? <Text> </Text> : null}
      {art.map((line, index) => renderOutlineLine(line, `whale-outline-${index}`))}
      <Text color={TONE.brand}>
        {waveLine(width, wavePad, shiftedWave(WAVE_FRONT, wave.length, frame, 1))}
      </Text>
      {bobDown ? null : <Text> </Text>}
    </Box>
  );
}

const BuddyCaption = React.memo(function BuddyCaption({
  config,
  mood,
}: {
  config: ResolvedBuddyConfig;
  mood: BuddyMood;
}): React.ReactElement {
  const message = config.muted ? "quiet" : buddyPhrase(mood);
  return (
    <Box>
      <Text color={FG.faint}>{config.name}</Text>
      <Text color={FG.faint}>{" | "}</Text>
      <Text color={moodColor(mood)}>{message}</Text>
    </Box>
  );
});

export function BuddySprite({ config, mood, pulse }: BuddySpriteProps): React.ReactElement | null {
  const { stdout } = useStdout();
  const cols = stdout?.columns ?? process.stdout.columns ?? 80;
  if (!config.enabled) return null;

  const currentMood = applyBuddyPulseToMood(mood, pulse);
  const compact = cols < 64;
  const width = compact ? COMPACT_WIDTH : WHALE_WIDTH;
  return (
    <Box
      flexDirection="column"
      alignItems="flex-end"
      alignSelf="flex-end"
      width={width}
      marginBottom={0}
    >
      <AnimatedWhaleScene mood={mood} pulse={pulse} compact={compact} />
      <BuddyCaption config={config} mood={currentMood} />
      {pulse?.kind === "wake" ? <Text color={TONE.accent}>{WAKE}</Text> : null}
    </Box>
  );
}
