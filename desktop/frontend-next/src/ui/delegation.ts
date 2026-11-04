import type { Tool } from "../port/wire";
import { t } from "../i18n";
import type { SubagentState } from "../port/subagent";
import { toolFailed } from "./cards/outcome";

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
  if (count && count > 1) return t("subagent.n_subagents_steps_steps", { n: count, steps });
  if (who) return t("subagent.name_took_n_steps", { name: who, n: steps });
  return t("subagent.the_subagent_took_n_steps", { n: steps });
}

// What a child ran on, as the kernel recorded it when the run started: the
// provider/model reference and the effort. The full reference is the title.
export const ranLabel = (p: Tool["profile"]): string => (p?.model ? [p.model, p.effort].filter(Boolean).join(" · ") : "");

// The same, for a row with no room: the model name is what tells two runs apart,
// so it is all the row keeps ("…/deepseek-v4-pro"); the provider and the effort are
// the title.
export const ranShort = (p: Tool["profile"]): string => {
  if (!p?.model) return "";
  const at = p.model.lastIndexOf("/");
  return at >= 0 ? `…/${p.model.slice(at + 1)}` : p.model;
};

export type DelegateStatus = "running" | "refused" | "failed" | "done";

// A host refusal names a call that never started; a failure is one that began
// and ended in error.
export const delegateStatus = (running: boolean, tool: Tool): DelegateStatus =>
  running ? "running" : tool.refusalCode ? "refused" : toolFailed(tool) ? "failed" : "done";

export const delegateStatusLabel = (status: DelegateStatus): string =>
  status === "running" ? t("common.running") : status === "refused" ? t("subagent.state.not_run") : status === "failed" ? t("common.failed") : t("subagent.state.delivered");

// What the kernel's own record says about one run, as opposed to the card's
// reading above, which has only the progress stream to go on.
export const delegateStateLabel = (state: SubagentState): string =>
  state === "running" ? t("common.running") : state === "completed" ? t("common.done_2") : state === "cancelled" ? t("common.cancelled") : state === "failed" ? t("common.failed") : t("common.interrupted");
