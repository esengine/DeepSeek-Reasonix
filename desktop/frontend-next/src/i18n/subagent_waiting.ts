import { t } from "./index";

export const SUBAGENT_WAITING = "subagent_waiting";

interface Waiting {
  task?: string;
  by?: string;
  whole?: boolean;
}

// The typed payload a waiting notice carries as its detail, read as the
// variables its sentence is worded from. A detail that is not one reads as none,
// so the notice keeps the kernel's English.
export function subagentWaitingVars(detail: string | undefined): Record<string, string> | undefined {
  if (!detail) return undefined;
  let p: Waiting;
  try {
    p = JSON.parse(detail) as Waiting;
  } catch {
    return undefined;
  }
  if (typeof p.task !== "string" || p.task === "") return undefined;
  const by = typeof p.by === "string" && p.by !== "" ? `「${p.by}」` : t("subagentWaiting.anotherSubAgent");
  const why = p.whole === true ? t("subagentWaiting.neitherDeclaredWritePathsSo") : t("subagentWaiting.theyWantToWriteThe");
  return { task: p.task, by, why };
}
