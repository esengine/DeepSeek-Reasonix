import { t } from "../i18n";
import { SettingRow } from "./SettingRow";
import { Path } from "./Path";

export function SessionDirSettings({ cwd, busy, onIsolate }: { cwd?: string; busy: string; onIsolate: () => void }) {
  return (
    <>
      <SettingRow id="session-dir" title={t("session.where_this_session_writes")} desc={t("settings.where_session_records_go")} more={t("session.folders_are_managed_in_the")}>
        <span className="srow-val"><Path of={cwd ?? "—"} /></span>
      </SettingRow>
      <div className="srow-more">
        <div className="lrow">
          <span className="tx">
            <span className="lb">{t("session.work_on_an_isolated_copy")}</span>
            <span className="ds">{t("session.work_in_a_separate_git")}</span>
          </span>
          <button className="spill" data-action="workspace.isolate" disabled={!!busy} onClick={onIsolate}>
            {t(busy === "isolate" ? "session.opening" : "session.open_one")}
          </button>
        </div>
      </div>
    </>
  );
}
