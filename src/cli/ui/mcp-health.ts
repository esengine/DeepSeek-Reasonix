import { COLOR } from "./theme.js";

export interface HealthBadge {
  glyph: string;
  label: string;
  color: string;
}

export function healthBadge(elapsedMs: number): HealthBadge {
  if (elapsedMs === 0) return { glyph: "✗", label: "no inspect data", color: COLOR.err };
  if (elapsedMs < 500) return { glyph: "●", label: `healthy · ${elapsedMs}ms`, color: COLOR.ok };
  if (elapsedMs < 3000) return { glyph: "◌", label: `slow · ${elapsedMs}ms`, color: COLOR.warn };
  return { glyph: "✗", label: `very slow · ${elapsedMs}ms`, color: COLOR.err };
}
