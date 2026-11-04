import { t } from "../i18n";
import type { LegacySkip, LegacySkipReason } from "../port/hub";
import { CopyButton } from "./CopyButton";

const REASON: Record<LegacySkipReason, string> = {
  too_large: "体积超过读取上限",
  unreadable_format: "不是本版本能读取的会话格式",
  schema_unsupported: "来自本版本尚不支持的存储版本",
  permission: "没有读取权限",
  corrupt: "文件已损坏",
  copy_failed: "复制到新位置时失败",
  no_project: "没有记录它属于哪个项目，所以没有导入",
};

export function LegacySkips({ skipped }: { skipped: LegacySkip[] }) {
  if (skipped.length === 0) return null;
  return (
    <details className="legacy-skips">
      <summary>{t("ext.n_sessions_were_not_imported", { n: skipped.length })}</summary>
      <span className="hint">{t("ext.these_files_are_still_where")}</span>
      {skipped.map((skip) => (
        <div className="item" key={skip.path}>
          <div className="l">
            <b>{skip.name}</b>
            <span className="hint">{t(REASON[skip.reason] ?? REASON.copy_failed)}</span>
            <span className="p">{skip.path}</span>
            <CopyButton text={skip.path} label={t("common.copy_path")} ariaLabel={t("ext.copy_path_name", { name: skip.name })} />
          </div>
        </div>
      ))}
    </details>
  );
}
