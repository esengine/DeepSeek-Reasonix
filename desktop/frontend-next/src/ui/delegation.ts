import type { Tool } from "../port/wire";
import { t } from "../i18n";

// One reading of "what left this context". The kernel marks a dispatch with a
// profile; the tool name cannot say it, because every dispatcher is reached
// through the use_capability proxy and the card carries the proxy's name.
export const isDelegation = (tool: Tool): boolean => tool.profile != null;

// A dispatch opens at least one context; a batch opens as many as it declared.
export const agentsOf = (tool: Tool): number => Math.max(tool.profile?.count ?? 1, 1);

// The rows nested under a dispatch are the delegate's own calls — steps, not
// delegates. Counting them as delegates is what turned one sub-agent doing 64
// things into "64 subagents"; the number of delegates only ever comes from the
// dispatch itself.
export function nestLabel(who: string | undefined, count: number | undefined, steps: number): string {
  if (count && count > 1) return t("{n} 个子代理 · {steps} 步", { n: count, steps });
  if (who) return t("{name} 做了 {n} 步", { name: who, n: steps });
  return t("子代理做了 {n} 步", { n: steps });
}
