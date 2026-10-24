import type { Item } from "../../state/session";
import { t } from "../../i18n";
import { leftoverDetail, noticeWording } from "../../i18n/notice_wording";
import { workspaceLeaseDetail, workspaceLeaseTitle } from "../../i18n/workspace_lease";
import { StudioIcon } from "../StudioIcon";
import { LazyMarkdown } from "../LazyMarkdown";
import { settingScreen } from "../settingScreens";

// A host notice is a bar, not a card: one sentence with a glyph, and under it
// the diagnostic the kernel wrote beside the headline it wrote for a person.
// Severity is the glyph's ink and nothing else — no box, no wash, no side rule.
// These name a rule as their detail: a command or target, set as code.
const PERMISSION = new Set(["permission_saved", "permission_covered", "setting_changed", "setting_open"]);
// These carry what the model wrote as their detail, so it reads the way the
// model's own replies do.
const AUTHORED = new Set(["await_user"]);
export function NoticeCard({ item, onSettings }: { item: Extract<Item, { t: "notice" }>; onSettings?: (at?: string) => void }) {
  const lvl = item.level === "error" ? "err" : item.level === "warn" ? "warn" : undefined;
  // The kernel writes in English for its own logs. Where this build has the
  // same notice in the reader's language, that is the one to show.
  const wording = noticeWording(item.code, item.detail);
  const vars = wording?.vars;
  const claim = item.workspaceLease;
  const detail = claim
    ? workspaceLeaseDetail(claim, item.code !== "workspace_lease_resumed") || item.detail
    : leftoverDetail(item.code, item.detail, vars);
  return (
    <div className="notice-bar" data-lvl={lvl ?? "info"} role={lvl === "err" ? "alert" : "status"}>
      <span className="sr-only">{t(lvl === "err" ? "出错" : lvl === "warn" ? "警告" : "提示")}：</span>
      {lvl ? <StudioIcon name="warning" className="nb-ic" aria-hidden="true" /> : <i className="nb-dot" aria-hidden="true" />}
      <div className="nb-body">
        <span className="t">
          {workspaceLeaseTitle(claim) ?? (wording ? t(wording.text, vars) : item.text)}
          {/* Repeats are folded rather than stacked: three identical lines
              say the same thing as one and a count, and bury whatever came
              before them. */}
          {item.count && item.count > 1 ? <b className="ntimes">×{item.count}</b> : null}
        </span>
        {item.code === "setting_open" && onSettings && (
          <button className="nbtn" data-action="settings.section" data-value={item.detail ?? ""} onClick={() => onSettings(settingScreen(item.detail ?? ""))}>{t("打开设置")}</button>
        )}
        {detail && (PERMISSION.has(item.code ?? "")
          ? <code className="nrule" title={item.text}>{item.detail}</code>
          : AUTHORED.has(item.code ?? "")
            ? <div className="nmd"><LazyMarkdown text={detail} /></div>
            : <span className="nwhy">{detail}</span>)}
      </div>
    </div>
  );
}
