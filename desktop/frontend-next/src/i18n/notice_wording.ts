import { t } from "./index";
import { FOLD_WHY, NO_CODE_WHY } from "./compaction_why";
import { COMPACTION_TELEMETRY, compactionNoticeVars } from "./compaction_notice";
import { CONTEXT_COMPOSITION, contextCompositionVars } from "./context_composition";
import { SUBAGENT_WAITING, subagentWaitingVars } from "./subagent_waiting";
import { EXTENSION_SKIPPED, extensionSkippedVars } from "./extension_skipped";
import { INBOX_RECOVERED, inboxRecoveredVars } from "./inbox_recovered";
import { JOB_FAILED, JOB_FINISHED, JOB_STARTED, JOB_KILLED, JOB_STALLED, jobErrorText, jobNoticeVars, jobStalledVars } from "./job_notice";
import { NOTICE_TEXT } from "./notices";
import { PERMISSION_RULES_DORMANT, permissionRulesDormantVars } from "./permission_rules_dormant";

export type NoticeVars = Record<string, string | number>;

const compactReason = (code: string) => (detail: string | undefined): NoticeVars | undefined => {
  const why = detail ? FOLD_WHY[detail] : code === "compact_declined" ? NO_CODE_WHY : undefined;
  return why ? { why: t(why) } : undefined;
};

// Every code whose sentence has a placeholder reads its values from the detail
// here; a placeholder code missing from this table fails notice_wording.test.ts.
const VARS: Record<string, (detail: string | undefined) => NoticeVars | undefined> = {
  [EXTENSION_SKIPPED]: extensionSkippedVars,
  [INBOX_RECOVERED]: inboxRecoveredVars,
  [PERMISSION_RULES_DORMANT]: permissionRulesDormantVars,
  [JOB_STARTED]: jobNoticeVars,
  [JOB_FINISHED]: jobNoticeVars,
  [JOB_KILLED]: jobNoticeVars,
  [JOB_FAILED]: jobNoticeVars,
  [JOB_STALLED]: jobStalledVars,
  [COMPACTION_TELEMETRY]: compactionNoticeVars,
  [CONTEXT_COMPOSITION]: contextCompositionVars,
  [SUBAGENT_WAITING]: subagentWaitingVars,
  display_currency: (detail) => ({ mode: detail || "auto" }),
  compact_declined: compactReason("compact_declined"),
  compact_failed: compactReason("compact_failed"),
  compact_held: compactReason("compact_held"),
};

export const NOTICE_VAR_CODES: readonly string[] = Object.keys(VARS);

// The sentence for a kernel code in this window's language, with the values its
// placeholders need. Undefined when the code has no sentence or its detail does
// not carry the values, so the caller keeps the kernel's own English.
export function noticeWording(code: string | undefined, detail: string | undefined): { text: string; vars?: NoticeVars } | undefined {
  const text = code ? NOTICE_TEXT[code] : undefined;
  if (!code || !text) return undefined;
  const read = VARS[code];
  if (!read) return /\{\w+\}/.test(text) ? undefined : { text };
  const vars = read(detail);
  return vars ? { text, vars } : undefined;
}

// What of the detail is still worth drawing under the sentence: all of it when
// nothing was read from it, only the job's error text when it was a payload.
export function leftoverDetail(code: string | undefined, detail: string | undefined, vars: NoticeVars | undefined): string | undefined {
  if (!vars) return detail;
  return code === JOB_FAILED ? jobErrorText(detail) : undefined;
}
