import type { Item } from "../../state/session";
import { t } from "../../i18n";
import { NOTICE_TEXT } from "../../i18n/notices";
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
// These carry the figures their sentence needs as JSON in the detail; the
// sentence has placeholders for them, so the detail is not drawn a second time.
const FIGURES = new Set(["context_budget"]);
// The detail is the stored value itself, empty for the automatic mode; the
// sentence names it, so it is not drawn a second time either.
const STORED = new Set(["display_currency"]);

// The detail is a compaction code; the sentence names its reason, so a code this
// build cannot word leaves the kernel's own text standing.
const REASONED = new Set(["compact_declined", "compact_failed"]);

function figures(detail?: string): Record<string, number> | undefined {
  try {
    const v = JSON.parse(detail ?? "");
    return v && typeof v === "object" && !Array.isArray(v) ? v : undefined;
  } catch {
    return undefined;
  }
}

export function NoticeCard({ item }: { item: Extract<Item, { t: "notice" }> }) {
  const lvl = item.level === "error" ? "err" : item.level === "warn" ? "warn" : undefined;
  // The kernel writes in English for its own logs. Where this build has the
  // same notice in the reader's language, that is the one to show.
  const stored = item.code !== undefined && STORED.has(item.code);
  const reasoned = item.code !== undefined && REASONED.has(item.code);
  const reason = reasoned ? (item.detail ? FOLD_WHY[item.detail] : item.code === "compact_declined" ? NO_CODE_WHY : undefined) : undefined;
  const vars: Record<string, string | number> | undefined = item.code && FIGURES.has(item.code) ? figures(item.detail) : stored ? { mode: item.detail || "auto" } : reason ? { why: t(reason) } : undefined;
  const wording = item.code && ((!FIGURES.has(item.code) && !reasoned) || vars) ? NOTICE_TEXT[item.code] : undefined;
  const claim = item.workspaceLease;
  const detail = claim
    ? workspaceLeaseDetail(claim, item.code !== "workspace_lease_resumed") || item.detail
    : item.detail;
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
            {detail && !vars && !reasoned && (PERMISSION.has(item.code ?? "")
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
