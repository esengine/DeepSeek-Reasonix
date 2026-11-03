import { t } from "../i18n";
import type { LegacySkip, LegacySkipReason } from "../port/hub";
import { CopyButton } from "./CopyButton";

const REASON: Record<LegacySkipReason, string> = {
  too_large: "legacySkips.largerThanTheSizeLimit",
  unreadable_format: "legacySkips.notASessionFormatThis",
  schema_unsupported: "legacySkips.writtenByAStorageVersion",
  permission: "legacySkips.noPermissionToReadIt",
  corrupt: "legacySkips.theFileIsDamaged",
  copy_failed: "legacySkips.copyingItToTheNew",
  no_project: "legacySkips.itRecordsNoProjectIt",
};

export function LegacySkips({ skipped }: { skipped: LegacySkip[] }) {
  if (skipped.length === 0) return null;
  return (
    <details className="legacy-skips">
      <summary>{t("legacySkips.sessionsWereNotImported", { n: skipped.length })}</summary>
      <span className="hint">{t("legacySkips.theseFilesAreStillWhere")}</span>
      {skipped.map((skip) => (
        <div className="item" key={skip.path}>
          <div className="l">
            <b>{skip.name}</b>
            <span className="hint">{t(REASON[skip.reason] ?? REASON.copy_failed)}</span>
            <span className="p">{skip.path}</span>
            <CopyButton text={skip.path} label={t("common.copyPath")} ariaLabel={t("legacySkips.copyPath", { name: skip.name })} />
          </div>
        </div>
      ))}
    </details>
  );
}
