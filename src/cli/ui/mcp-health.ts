import { t } from "../../i18n/index.js";
import { COLOR } from "./theme.js";

export interface HealthBadge {
  glyph: string;
  label: string;
  color: string;
}

export function healthBadge(elapsedMs: number): HealthBadge {
  if (elapsedMs === 0) return { glyph: "✗", label: t("mcpHealth.noData"), color: COLOR.err };
  if (elapsedMs < 500)
    return { glyph: "●", label: t("mcpHealth.healthy", { ms: elapsedMs }), color: COLOR.ok };
  if (elapsedMs < 3000)
    return { glyph: "◌", label: t("mcpHealth.slow", { ms: elapsedMs }), color: COLOR.warn };
  return { glyph: "✗", label: t("mcpHealth.verySlow", { ms: elapsedMs }), color: COLOR.err };
}

// Preserves original slash thresholds: 0 → "● healthy · 0ms" (no === 0 branch)
export function slashHealthBadge(elapsedMs: number): string {
  if (elapsedMs < 500) return t("mcpHealth.healthy", { ms: elapsedMs });
  if (elapsedMs < 3000) return t("mcpHealth.slow", { ms: elapsedMs });
  return t("mcpHealth.verySlow", { ms: elapsedMs });
}
