import { completionSummaryChangeNotice } from "../lib/completionSummary";
import type { DictKey, Translator } from "../lib/i18n";

const messages: Partial<Record<DictKey, string>> = {
  "notice.completionChangesTitle": "Changes this turn",
  "notice.completionChangesBody": "{count} changes",
};

const translate: Translator = (key, vars) => {
  const value = messages[key] ?? key;
  return value.replace(/\{(\w+)\}/g, (_, name: string) => String(vars?.[name] ?? `{${name}}`));
};

const notice = completionSummaryChangeNotice({
  preset: "balanced",
  verdict: "complete",
  mutations: 3,
  checks_passed: 0,
  checks_failed: 0,
  checks_suppressed: 0,
  review: "passed",
  constraint_degraded: false,
}, translate);

if (notice.body !== "3 changes" || notice.body.includes("file")) {
  throw new Error(`mutation receipt notice = ${notice.body}, want receipt-neutral changes wording`);
}
