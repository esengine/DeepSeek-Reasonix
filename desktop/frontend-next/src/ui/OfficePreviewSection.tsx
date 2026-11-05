import { useSyncExternalStore } from "react";
import { t } from "../i18n";
import { onOfficePreviewChange, previewsOffice, setPreviewsOffice } from "../state/prefs";
import { Group } from "./Group";
import { Switch } from "./Switch";

export function OfficePreviewSection() {
  const on = useSyncExternalStore(onOfficePreviewChange, previewsOffice, previewsOffice);
  return (
    <Group id="office-preview" inline title={t("common.office_document_preview")} hint={t("settings.preview_office_in_workbench")}>
      <Switch data-action="appearance.office-preview" on={on} label={t("common.office_document_preview")} onClick={() => setPreviewsOffice(!on)} />
    </Group>
  );
}
