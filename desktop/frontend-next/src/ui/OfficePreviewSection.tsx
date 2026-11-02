import { useSyncExternalStore } from "react";
import { t } from "../i18n";
import { onOfficePreviewChange, previewsOffice, setPreviewsOffice } from "../state/prefs";
import { Group } from "./Group";

export function OfficePreviewSection() {
  const on = useSyncExternalStore(onOfficePreviewChange, previewsOffice, previewsOffice);
  return (
    <Group id="office-preview" title={t("Office 文档预览")} hint={t("在工作台里只读预览 Word（.docx）和 Excel（.xlsx）。首次打开这类文件时才加载，在隔离的框中显示，不运行宏和脚本。关闭后只能用系统应用打开。仅保存在本机。")}>
      <div className="seg" data-text role="group" aria-label={t("Office 文档预览")}>
        <button data-action="appearance.office-preview" data-value="off" aria-pressed={!on} onClick={() => setPreviewsOffice(false)}>{t("关闭")}</button>
        <button data-action="appearance.office-preview" data-value="on" aria-pressed={on} onClick={() => setPreviewsOffice(true)}>{t("预览")}</button>
      </div>
    </Group>
  );
}
