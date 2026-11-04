import { t } from "../i18n";
import type { LegacySkip, LegacySkipReason } from "../port/hub";
import { CopyButton } from "./CopyButton";

const REASON: Record<LegacySkipReason, string> = {
  too_large: "ext.larger_than_the_size_limit",
  unreadable_format: "ext.not_a_session_format_this",
  schema_unsupported: "ext.written_by_a_storage_version",
  permission: "ext.no_permission_to_read_it",
  corrupt: "ext.the_file_is_damaged",
  copy_failed: "ext.copying_it_to_the_new",
  no_project: "ext.it_records_no_project_it",
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
