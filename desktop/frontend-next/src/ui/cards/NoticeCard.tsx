import type { Item } from "../../state/session";
import { t } from "../../i18n";
import { NOTICE_TEXT } from "../../i18n/notices";
import { EXTENSION_SKIPPED, extensionSkippedVars } from "../../i18n/extension_skipped";
import { PERMISSION_RULES_DORMANT, permissionRulesDormantVars } from "../../i18n/permission_rules_dormant";
import { INBOX_RECOVERED, inboxRecoveredVars } from "../../i18n/inbox_recovered";
import { JOB_FAILED, JOB_FINISHED, JOB_KILLED, jobErrorText, jobNoticeVars } from "../../i18n/job_notice";
import { FOLD_WHY, NO_CODE_WHY } from "../../i18n/compaction_why";
import { workspaceLeaseDetail } from "../../i18n/workspace_lease";
import { Sym } from "../Sym";
import { LazyMarkdown } from "../LazyMarkdown";

// One of the host's cards, so it is built like the others: a gutter, a headline
// naming the speaker, and the body under it. The body keeps the kernel's two
// halves apart — the headline it wrote for a person, and the diagnostic under
// it, which is what .why is for everywhere in this UI. Severity is a colour bar
// here as in .guard and .find, and the gutter follows it too.
// These name a rule as their detail: a command or target, set as code.
const PERMISSION = new Set(["permission_saved", "permission_covered"]);
// These carry what the model wrote as their detail, so it reads the way the
// model's own replies do.
const AUTHORED = new Set(["await_user"]);
// The detail is the stored value itself, empty for the automatic mode; the
// sentence names it, so it is not drawn a second time either.
const STORED = new Set(["display_currency"]);

// The detail is a compaction code; the sentence names its reason, so a code this
// build cannot word leaves the kernel's own text standing.
const REASONED = new Set(["compact_declined", "compact_failed", "compact_held"]);

export function NoticeCard({ item }: { item: Extract<Item, { t: "notice" }> }) {
  const lvl = item.level === "error" ? "err" : item.level === "warn" ? "warn" : undefined;
  // The kernel writes in English for its own logs. Where this build has the
  // same notice in the reader's language, that is the one to show.
  const stored = item.code !== undefined && STORED.has(item.code);
  const reasoned = item.code !== undefined && REASONED.has(item.code);
  const reason = reasoned ? (item.detail ? FOLD_WHY[item.detail] : item.code === "compact_declined" ? NO_CODE_WHY : undefined) : undefined;
  const skipped = item.code === EXTENSION_SKIPPED ? extensionSkippedVars(item.detail) : undefined;
  const recovered = item.code === INBOX_RECOVERED ? inboxRecoveredVars(item.detail) : undefined;
  const job = item.code === JOB_FINISHED || item.code === JOB_KILLED || item.code === JOB_FAILED ? jobNoticeVars(item.detail) : undefined;
  const dormant = item.code === PERMISSION_RULES_DORMANT ? permissionRulesDormantVars(item.detail) : undefined;
  const vars: Record<string, string | number> | undefined = stored ? { mode: item.detail || "auto" } : reason ? { why: t(reason) } : skipped ?? recovered ?? job ?? dormant;
  const wording = item.code && (!reasoned || vars) && (item.code !== EXTENSION_SKIPPED || skipped) && (item.code !== INBOX_RECOVERED || recovered) && (item.code !== PERMISSION_RULES_DORMANT || dormant) && ((item.code !== JOB_FINISHED && item.code !== JOB_KILLED && item.code !== JOB_FAILED) || job) ? NOTICE_TEXT[item.code] : undefined;
  const claim = item.workspaceLease;
  const detail = claim
    ? workspaceLeaseDetail(claim, item.code !== "workspace_lease_resumed") || item.detail
    : job && item.code === JOB_FAILED ? jobErrorText(item.detail) : item.detail;
  return (
    <div className="call" data-k="host" data-lvl={lvl}>
      <div className="g">
        <Sym glyph="·" />
        <span className="line" />
      </div>
      <div className="c">
        <div className="hl">
          <span className="nm">{t(lvl === "err" ? "出错" : lvl === "warn" ? "警告" : "提示")}</span>
          <span className="tag">{t("主机")}</span>
        </div>
        <div className="out">
          <div className="find" data-lvl={lvl}>
            <span className="t">
              {wording ? t(wording, vars) : item.text}
              {/* Repeats are folded rather than stacked: three identical lines
                  say the same thing as one and a count, and bury whatever came
                  before them. */}
              {item.count && item.count > 1 ? <b className="ntimes">×{item.count}</b> : null}
            </span>
            {detail && (!vars || (job && item.code === JOB_FAILED)) && !reasoned && (PERMISSION.has(item.code ?? "")
              ? <code className="nrule" title={item.text}>{item.detail}</code>
              : AUTHORED.has(item.code ?? "")
                ? <div className="nmd"><LazyMarkdown text={detail} /></div>
                : <span className="why nwhy">{detail}</span>)}
          </div>
        </div>
      </div>
    </div>
  );
}
