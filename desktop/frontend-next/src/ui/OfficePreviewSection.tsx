import { useSyncExternalStore } from "react";
import { t } from "../i18n";
import { onOfficePreviewChange, previewsOffice, setPreviewsOffice } from "../state/prefs";
import { Group } from "./Group";

export function OfficePreviewSection() {
  const on = useSyncExternalStore(onOfficePreviewChange, previewsOffice, previewsOffice);
  return (
    <Group id="office-preview" inline title={t("common.office_document_preview")} hint={t("settings.preview_office_in_workbench")}>
      <div className="seg" data-text role="group" aria-label={t("common.office_document_preview")}>
        <button data-action="appearance.office-preview" data-value="off" aria-pressed={!on} onClick={() => setPreviewsOffice(false)}>{t("common.close")}</button>
        <button data-action="appearance.office-preview" data-value="on" aria-pressed={on} onClick={() => setPreviewsOffice(true)}>{t("common.preview")}</button>
      </div>
    </Group>
  );
}
