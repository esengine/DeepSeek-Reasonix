import type { Item } from "./useController";
import { t } from "./i18n";

export interface WireReadPause {
	code?: string;
	id: string;
	reads: { readId: string; path: string; snapshot?: string; intent?: string; covered?: [number, number][]; missing?: [number, number][]; reason: string }[];
  omitted?: number;
}

function rangesLabel(ranges: [number, number][] | undefined): string {
  return (Array.isArray(ranges) ? ranges : []).slice(0, 64)
    .filter(r => Array.isArray(r) && Number.isInteger(r[0]) && Number.isInteger(r[1]) && r[0] >= 0 && r[1] > r[0])
    .map(([start, end]) => `${start + 1}–${end}`).join(", ");
}

export function readPauseItem(pause: WireReadPause | undefined, fallbackID: string): Item {
  const reasons = new Set<string>();
  const details = (Array.isArray(pause?.reads) ? pause.reads : []).slice(0, 32).map(read => {
    const reasonKey = read.reason === "no_progress" ? "composer.readStatusStalled"
      : ["page_budget", "time_budget", "no_headroom", "unknown_window"].includes(read.reason) ? "composer.readStatusBudget" : "composer.readStatusSource";
    const covered = rangesLabel(read.covered);
    const missing = rangesLabel(read.missing);
    reasons.add(t(reasonKey));
    return [read.path, t(reasonKey), covered && t("notice.readPauseCovered", { range: covered }), missing && t("notice.readPauseMissing", { range: missing })].filter(Boolean).join(" · ");
  });
  if (pause?.omitted) details.push(t("notice.readPauseOmitted", { count: pause.omitted }));
  return { kind: "notice", id: pause?.id ? `read-pause-${pause.id}` : fallbackID, level: "info", code: "incomplete_read", title: t("notice.readPauseTitle"), text: [...reasons, t("notice.readPauseBody")].join(" "), detail: details.join("\n") };
}

export function upsertReadPause(items: Item[], pause: WireReadPause | undefined, fallbackID: string): Item[] {
  const item = readPauseItem(pause, fallbackID);
  const index = items.findIndex(existing => existing.id === item.id);
  if (index < 0) return [...items, item];
  return items.map((existing, i) => i === index ? item : existing);
}
