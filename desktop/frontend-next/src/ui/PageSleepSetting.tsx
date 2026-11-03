import { t } from "../i18n";
import { setPageSleep, usePageSleep } from "../state/pagesleep";
import { Group } from "./Group";

export function PageSleepSetting() {
  const mode = usePageSleep();
  return (
    <Group id="page-sleep" title={t("common.idlePageSleep")} hint={t("pageSleepSetting.aPageNothingHasDriven")}>
      <div className="seg" data-text role="radiogroup" aria-label={t("common.idlePageSleep")}>
        <button role="radio" data-action="appearance.page-sleep" data-value="off" aria-checked={mode === "off"} onClick={() => setPageSleep("off")}>{t("pageSleepSetting.keepOpen")}</button>
        <button role="radio" data-action="appearance.page-sleep" data-value="on" aria-checked={mode === "on"} onClick={() => setPageSleep("on")}>{t("pageSleepSetting.onAfter15Minutes")}</button>
      </div>
    </Group>
  );
}
