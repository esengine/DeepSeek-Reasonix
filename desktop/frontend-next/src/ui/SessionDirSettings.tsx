import { t } from "../i18n";
import { Group } from "./Group";
import { Path } from "./Path";

export function SessionDirSettings({ cwd, busy, onIsolate }: { cwd?: string; busy: string; onIsolate: () => void }) {
  return (
    <Group id="session-dir" title={t("session.where_this_session_writes")}>
      <div className="kv">
        <span className="k">{t("session.working_directory")}</span>
        <span className="v"><Path of={cwd ?? "—"} /></span>
      </div>
      <p className="note">
        {t("session.folders_are_managed_in_the")}
      </p>
      <div className="lrow">
        <span className="tx">
          <span className="lb">{t("session.work_on_an_isolated_copy")}</span>
          <span className="ds">{t("session.work_in_a_separate_git")}</span>
        </span>
        <button className="act" data-action="workspace.isolate" disabled={!!busy} onClick={onIsolate}>
          {t(busy === "isolate" ? "session.opening" : "session.open_one")}
        </button>
      </div>
    </Group>
  );
}
