import { t } from "../i18n";
import { Group } from "./Group";
import { Path } from "./Path";

export function SessionDirSettings({ cwd, busy, onIsolate }: { cwd?: string; busy: string; onIsolate: () => void }) {
  return (
    <Group id="session-dir" title={t("会话写入位置")}>
      <div className="kv">
        <span className="k">{t("工作目录")}</span>
        <span className="v"><Path of={cwd ?? "—"} /></span>
      </div>
      <p className="note">
        {t("在左栏管理文件夹：底部添加，展开后可新建会话。会话归属于创建它的文件夹，不会移动到其他位置。")}
      </p>
      <div className="lrow">
        <span className="tx">
          <span className="lb">{t("创建隔离副本")}</span>
          <span className="ds">{t("在 Git worktree 中创建独立副本，修改不会影响当前分支")}</span>
        </span>
        <button className="act" data-action="workspace.isolate" disabled={!!busy} onClick={onIsolate}>
          {t(busy === "isolate" ? "启用中…" : "创建")}
        </button>
      </div>
    </Group>
  );
}
