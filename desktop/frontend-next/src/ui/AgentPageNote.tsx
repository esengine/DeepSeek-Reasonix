import { t } from "../i18n";
import type { BrowserTab } from "../port/port";

const LONGEST = 500;
const UNSEEN = /[\u0000-\u0008\u000E-\u001F\u007F-\u009F\u061C\u200B-\u200F\u202A-\u202E\u2060-\u2064\u2066-\u2069\uFEFF]/g;

function plain(text: string): string {
  const clean = text.replace(UNSEEN, "").replace(/\s+/g, " ").trim();
  const marks = Array.from(clean);
  return marks.length > LONGEST ? `${marks.slice(0, LONGEST).join("")}…` : clean;
}

export function AgentPageNote({ tab }: { tab: BrowserTab }) {
  const url = plain(tab.url);
  return (
    <div className="bpage" role="region" aria-label={t("智能体正在查看的页面")}>
      <b data-page-title dir="auto">{plain(tab.title) || t("空白页")}</b>
      {url && <code data-page-url dir="ltr">{url}</code>}
      <p>{t("只读：页面显示在运行智能体的电脑上")}</p>
    </div>
  );
}
