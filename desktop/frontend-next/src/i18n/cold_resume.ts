import type { ColdResume } from "../port/cold_resume";
import { elapsed, tokens } from "./format";
import { t } from "./index";

export const COLD_RESUME = "cold_resume";

function read(detail: string | undefined): ColdResume | undefined {
  if (!detail) return undefined;
  try {
    const p = JSON.parse(detail) as Partial<ColdResume>;
    if (typeof p.idle_seconds !== "number" || p.idle_seconds <= 0 || typeof p.tokens !== "number") return undefined;
    return { idle_seconds: p.idle_seconds, tokens: p.tokens, window: p.window, compact: p.compact === true };
  } catch {
    return undefined;
  }
}

// The typed payload a cold-resume notice carries as its detail, read as the
// variables its sentence is worded from. A detail that is not one reads as none,
// so the notice keeps the kernel's English.
export function coldResumeVars(detail: string | undefined): Record<string, string> | undefined {
  const p = read(detail);
  if (!p) return undefined;
  const more = p.compact ? t("chat.the_next_request_resends_about", { n: tokens(p.tokens) }) : t("chat.the_full_history_is_kept");
  return { idle: elapsed(p.idle_seconds * 1000), more };
}

// Whether the host offered a fold with this notice: the payload says, never the sentence.
export function coldResumeOffersCompact(detail: string | undefined): boolean {
  return read(detail)?.compact === true;
}
