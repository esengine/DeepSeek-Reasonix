import { readFileSync } from "node:fs";
import { activeWorkBusyNoticeText } from "../lib/capabilityMutations";
import { t } from "../lib/i18n";

function ok(value: unknown, message: string) {
  if (!value) throw new Error(message);
}

const activeWorkError = (running: boolean, pendingPrompt: boolean, backgroundJobs: number) =>
  new Error(`active work is still running; running=${running}; pending_prompt=${pendingPrompt}; background_jobs=${backgroundJobs}; finish or cancel the current turn, answer pending prompts, and stop background jobs before changing MCP server`);

ok(
  activeWorkBusyNoticeText(activeWorkError(true, false, 0), t) === "This setting cannot change while the current answer is running. Stop it first.",
  "capability mutations localize a running answer without exposing debug fields",
);
ok(
  activeWorkBusyNoticeText(activeWorkError(true, true, 0), t) === "This setting cannot change while a confirmation is waiting. Handle it first.",
  "a pending prompt takes priority over the running-answer explanation",
);
ok(
  activeWorkBusyNoticeText(activeWorkError(false, false, 2), t) === "2 background jobs are still running. Stop them before changing this setting.",
  "capability mutations report the blocking background-job count",
);
ok(
  activeWorkBusyNoticeText(
    new Error("active work is still running; finish or cancel the current turn before changing MCP server"),
    t,
  ) === "This setting cannot change yet. Stop the current answer, handle pending prompts, or wait for background jobs to finish.",
  "busy errors without structured details use a friendly generic explanation",
);
ok(
  activeWorkBusyNoticeText(new Error("MCP server executable was not found"), t) === null,
  "non-busy capability failures keep their original actionable error",
);
const capabilitiesSource = readFileSync(new URL("../components/CapabilitiesPanel.tsx", import.meta.url), "utf8");
ok(
  (capabilitiesSource.match(/setErr\(activeWorkBusyNoticeText\(e, t\) \?\?/g) ?? []).length === 4,
  "all four capability mutation surfaces localize active-work failures",
);
