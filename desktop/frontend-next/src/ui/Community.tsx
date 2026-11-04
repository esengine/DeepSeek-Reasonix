import { useMemo, useState } from "react";
import { t } from "../i18n";
import type { AgentPort } from "../port/port";
import contributors from "../data/contributors.json";
import qrSrc from "../assets/qq-group-qr.svg";
import douyinSrc from "../assets/douyin-qr.jpg";
import { CopyButton } from "./CopyButton";
import { COMMUNITY, profileUrl } from "./communityLinks";

const COLLAPSED = 24;

export function Community({ port, logins = contributors }: { port: Pick<AgentPort, "openExternal">; logins?: string[] }) {
  const [all, setAll] = useState(false);
  const [failed, setFailed] = useState("");
  const [douyinQr, setDouyinQr] = useState(false);

  const open = (url: string) =>
    void port.openExternal(url).then(() => setFailed(""), () => setFailed(t("common.could_not_open_a_browser", { at: url })));

  const people = useMemo(
    () => logins.flatMap((login) => { const url = profileUrl(login); return url ? [{ login, url }] : []; }),
    [logins],
  );
  const shown = all ? people : people.slice(0, COLLAPSED);

  return (
    <div className="comm">
      <div className="comm-join">
        <img className="comm-qr" src={qrSrc} alt={t("feedback.qq_group_qr_code")} width={185} height={185} />
        <div className="comm-qq">
          <span className="comm-name">{COMMUNITY.qqName}</span>
          <span className="comm-num">
            <span className="comm-k">{t("feedback.group_number")}</span>
            <code>{COMMUNITY.qqNumber}</code>
            <CopyButton text={COMMUNITY.qqNumber} iconOnly showFeedback label={t("feedback.copy_group_number")} />
          </span>
          <span className="comm-how">{t("feedback.scan_the_qr_code_with")}</span>
          <span className="comm-acts">
            <button type="button" className="btn" data-primary data-action="community.join" onClick={() => open(COMMUNITY.qqJoin)}>{t("feedback.join_the_qq_group")}</button>
            <button type="button" className="btn" data-action="community.discord" onClick={() => open(COMMUNITY.discord)}>Discord</button>
            <button type="button" className="btn" data-action="community.issues" onClick={() => open(COMMUNITY.issues)}>GitHub issues</button>
          </span>
        </div>
      </div>

      <div className="comm-dy">
        <span className="comm-k">{t("feedback.douyin")}</span>
        <span className="comm-dy-name">{COMMUNITY.douyinName}</span>
        <span className="comm-k">{t("feedback.douyin_id")}</span>
        <code>{COMMUNITY.douyinId}</code>
        <CopyButton text={COMMUNITY.douyinId} iconOnly showFeedback label={t("feedback.copy_douyin_id")} />
        <button type="button" className="btn sm" data-action="community.douyin-qr" aria-expanded={douyinQr} aria-controls="comm-dy-qr" onClick={() => setDouyinQr((v) => !v)}>
          {douyinQr ? t("feedback.hide_qr_code") : t("feedback.show_qr_code")}
        </button>
      </div>
      {douyinQr && (
        <img id="comm-dy-qr" className="comm-dy-qr" src={douyinSrc} alt={t("feedback.douyin_qr_code")} width={180} height={269} />
      )}

      {failed && <p className="comm-fail" role="alert">{failed}</p>}

      <div className="comm-who">
        <div className="comm-who-hd">
          <h4>{t("feedback.contributors")}</h4>
          <span className="comm-k">{t("feedback.sorted_by_commits_the_list")}</span>
          <button type="button" className="btn sm" data-action="community.contributors" onClick={() => open(COMMUNITY.contributors)}>{t("feedback.view_all_contributors")}</button>
        </div>
        <ul className="comm-list" aria-label={t("feedback.contributors")}>
          {shown.map(({ login, url }) => (
            <li key={login}>
              <a href={url} data-action="community.profile" data-target={login} onClick={(e) => { e.preventDefault(); open(url); }}>{login}</a>
            </li>
          ))}
        </ul>
        {people.length > COLLAPSED && (
          <button type="button" className="btn sm comm-more" data-action="community.more" aria-expanded={all} onClick={() => setAll((v) => !v)}>
            {all ? t("common.collapse") : t("feedback.show_all_n", { n: people.length })}
          </button>
        )}
      </div>
    </div>
  );
}
