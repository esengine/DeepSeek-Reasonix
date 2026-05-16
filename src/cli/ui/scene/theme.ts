import type { Color } from "./types.js";

export const PALETTE = {
  bg: { hex: "#1a1c22" },
  bg2: { hex: "#1f2228" },
  panel: { hex: "#23272e" },
  card: { hex: "#272c36" },
  border: { hex: "#393f4a" },
  borderStrong: { hex: "#4a515f" },
  fg: { hex: "#ecedf2" },
  fg2: { hex: "#c2c6d0" },
  muted: { hex: "#8d94a3" },
  accent: { hex: "#648dff" },
  accentStrong: { hex: "#7da0ff" },
  success: { hex: "#4bc88a" },
  warning: { hex: "#d8b157" },
  danger: { hex: "#e25a5a" },
  violet: { hex: "#b87dde" },
} as const satisfies Record<string, Color>;
