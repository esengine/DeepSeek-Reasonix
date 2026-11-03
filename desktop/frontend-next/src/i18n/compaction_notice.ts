import { tokens } from "./format";
import { t } from "./index";

export const COMPACTION_TELEMETRY = "compaction_telemetry";

const HOW: Record<string, string> = {
  summarized: "compactionNotice.summarized",
  degraded: "compactionNotice.degradedNoSummaryAvailable",
  native: "compactionNotice.compactedByTheProvider",
  snip: "compactionNotice.oldToolOutputTrimmed",
};

interface Fold {
  mode: string;
  source_tokens: number;
  projection_tokens: number;
  input_tokens: number;
  output_tokens: number;
  cache_hit_tokens: number;
  request_count: number;
}

// The typed payload a finished-fold notice carries as its detail, read as the
// variables its sentence is worded from. A detail that is not one reads as none,
// so the notice keeps the kernel's English.
export function compactionNoticeVars(detail: string | undefined): Record<string, string> | undefined {
  if (!detail) return undefined;
  let p: Partial<Fold>;
  try {
    p = JSON.parse(detail) as Partial<Fold>;
  } catch {
    return undefined;
  }
  if (typeof p.mode !== "string" || p.mode === "" || typeof p.source_tokens !== "number" || typeof p.projection_tokens !== "number") return undefined;
  const asked = typeof p.request_count === "number" && p.request_count > 0 && typeof p.input_tokens === "number" && typeof p.output_tokens === "number";
  const usage = asked
    ? t("compactionNotice.theSummaryRequestUsedInput", { in: tokens(p.input_tokens!), out: tokens(p.output_tokens!), hit: tokens(p.cache_hit_tokens ?? 0) })
    : "";
  return { how: t(HOW[p.mode] ?? p.mode), src: tokens(p.source_tokens), proj: tokens(p.projection_tokens), usage };
}
