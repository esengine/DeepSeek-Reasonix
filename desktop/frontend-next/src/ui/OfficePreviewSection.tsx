import { useSyncExternalStore } from "react";
import { t } from "../i18n";
import { onOfficePreviewChange, previewsOffice, setPreviewsOffice } from "../state/prefs";
import { Group } from "./Group";

export function OfficePreviewSection() {
  const on = useSyncExternalStore(onOfficePreviewChange, previewsOffice, previewsOffice);
  return (
    <Group id="office-preview" title={t("common.officeDocumentPreview")} hint={t("officePreviewSection.readOnlyPreviewOfWord")}>
      <div className="seg" data-text role="group" aria-label={t("common.officeDocumentPreview")}>
        <button data-action="appearance.office-preview" data-value="off" aria-pressed={!on} onClick={() => setPreviewsOffice(false)}>{t("common.close")}</button>
        <button data-action="appearance.office-preview" data-value="on" aria-pressed={on} onClick={() => setPreviewsOffice(true)}>{t("common.preview")}</button>
      </div>
    </Group>
  );
}
