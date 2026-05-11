import { basename } from "node:path";
import { listPlanArchives, loadPlanState, relativeTime } from "@/code/plan-store.js";
import { t } from "@/i18n/index.js";
import type { SlashHandler } from "../dispatch.js";

const plans: SlashHandler = (args, loop, ctx) => {
  const sessionName = loop.sessionName;
  if (!sessionName) {
    return { info: t("handlers.plans.noSession") };
  }
  const sub = (args[0] ?? "").toLowerCase();
  if (sub === "done") {
    return handleDone(args.slice(1), ctx);
  }
  const lines: string[] = [];
  const active = loadPlanState(sessionName);
  if (active && active.steps.length > 0) {
    const total = active.steps.length;
    const done = active.completedStepIds.length;
    const when = relativeTime(active.updatedAt);
    const label = active.summary ? `: ${active.summary}` : "";
    lines.push(
      t("handlers.plans.activePlan", {
        label,
        done,
        total,
        s: total === 1 ? "" : "s",
        when,
      }),
    );
  } else {
    lines.push(t("handlers.plans.activeNone"));
  }

  const archives = listPlanArchives(sessionName);
  if (archives.length === 0) {
    lines.push("");
    lines.push(t("handlers.plans.noArchives"));
    return { info: lines.join("\n") };
  }
  lines.push("");
  lines.push(t("handlers.plans.archivedHeader", { count: archives.length }));
  for (const a of archives) {
    const when = relativeTime(a.completedAt);
    const total = a.steps.length;
    const done = a.completedStepIds.length;
    const completion = done >= total ? t("handlers.plans.completionComplete") : `${done}/${total}`;
    const label = a.summary ?? a.path.split(/[\\/]/).pop() ?? a.path;
    lines.push(
      t("handlers.plans.archivedRow", {
        when: when.padEnd(10),
        total,
        s: total === 1 ? "" : "s",
        completion,
        label,
      }),
    );
  }
  return { info: lines.join("\n") };
};

const replay: SlashHandler = (args, loop) => {
  const sessionName = loop.sessionName;
  if (!sessionName) {
    return { info: t("handlers.plans.replayNoSession") };
  }
  const archives = listPlanArchives(sessionName);
  if (archives.length === 0) {
    return { info: t("handlers.plans.replayNoArchives") };
  }
  const arg = args[0]?.trim() ?? "";
  const index = arg ? Number.parseInt(arg, 10) : 1;
  if (!Number.isFinite(index) || index < 1 || index > archives.length) {
    return {
      info: t("handlers.plans.replayInvalidIndex", { max: archives.length }),
    };
  }
  const a = archives[index - 1]!;
  return {
    replayPlan: {
      summary: a.summary,
      body: a.body,
      steps: a.steps,
      completedStepIds: a.completedStepIds,
      completedAt: a.completedAt,
      relativeTime: relativeTime(a.completedAt),
      archiveBasename: basename(a.path),
      index,
      total: archives.length,
    },
  };
};

const stop: SlashHandler = (_args, loop) => {
  loop.abort();
  return { info: t("handlers.plans.stopAborted") };
};

function handleDone(rest: string[], ctx: Parameters<SlashHandler>[2]): { info: string } {
  const target = (rest[0] ?? "").trim();
  if (!target) {
    return { info: t("handlers.plans.doneUsage") };
  }
  if (target.toLowerCase() === "all") {
    const fn = ctx.markAllPlanStepsDone;
    if (!fn) return { info: t("handlers.plans.doneUnavailable") };
    const added = fn();
    if (added === 0) return { info: t("handlers.plans.doneAllNoop") };
    return { info: t("handlers.plans.doneAllOk", { count: added }) };
  }
  const fn = ctx.markPlanStepDone;
  if (!fn) return { info: t("handlers.plans.doneUnavailable") };
  const outcome = fn(target);
  switch (outcome) {
    case "ok":
      return { info: t("handlers.plans.doneOk", { id: target }) };
    case "already-done":
      return { info: t("handlers.plans.doneAlready", { id: target }) };
    case "not-in-plan":
      return { info: t("handlers.plans.doneNotInPlan", { id: target }) };
    case "no-plan":
      return { info: t("handlers.plans.doneNoPlan") };
  }
}

export const handlers: Record<string, SlashHandler> = {
  plans,
  replay,
  stop,
};
