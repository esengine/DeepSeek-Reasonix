import { say } from "../i18n/kernel";
import { t } from "../i18n";
import type { ProviderEntry } from "../port/port";

export type ProviderDot = { state?: "use" | "warn" | "fail" | "pass"; title: string };

/** What the list row's dot says about an account. A failed test is the news, so
 *  it leads; then the standing facts in their old order (in use, no key); a
 *  passed test shows only when nothing else is being said. */
export function providerDot(entries: ProviderEntry[]): ProviderDot {
  const failed = entries.find((e) => e.lastCheck && !e.lastCheck.ok);
  if (failed) {
    const why = failed.lastCheck?.code ? say({ code: failed.lastCheck.code }, "") : "";
    return { state: "fail", title: why ? t("models.last_test_failed_why", { why }) : t("models.last_test_failed") };
  }
  if (entries.some((e) => e.inUse)) return { state: "use", title: t("models.in_use") };
  if (entries.every((e) => !e.hasKey)) return { state: "warn", title: t("models.no_key") };
  if (entries.some((e) => e.lastCheck?.ok)) return { state: "pass", title: t("models.last_test_passed") };
  return { title: "" };
}
