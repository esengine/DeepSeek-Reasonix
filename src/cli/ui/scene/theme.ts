import type { Color } from "./types.js";

export const PALETTE = {
  bg: { hex: "#0f1018" },
  bg2: { hex: "#161824" },
  fg: { hex: "#e8e9f3" },
  fg1: { hex: "#a8aabd" },
  fg2: { hex: "#6b6e85" },
  fg3: { hex: "#3d4055" },
  ds: { hex: "#6b85ff" },
  dsBright: { hex: "#8b9fff" },
  dsPurple: { hex: "#a78bfa" },
  ok: { hex: "#5eead4" },
  warn: { hex: "#fbbf24" },
  err: { hex: "#fb7185" },
  info: { hex: "#60a5fa" },
} as const satisfies Record<string, Color>;
