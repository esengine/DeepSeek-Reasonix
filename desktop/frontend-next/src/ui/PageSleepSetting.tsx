import { t } from "../i18n";
import { setPageSleep, usePageSleep } from "../state/pagesleep";
import { Group } from "./Group";

export function PageSleepSetting() {
  const mode = usePageSleep();
  return (
    <Group id="page-sleep" title={t("settings.idle_page_sleep")} hint={t("settings.a_page_nothing_has_driven")}>
      <div className="seg" data-text role="radiogroup" aria-label={t("settings.idle_page_sleep")}>
        <button role="radio" data-action="appearance.page-sleep" data-value="off" aria-checked={mode === "off"} onClick={() => setPageSleep("off")}>{t("settings.keep_open")}</button>
        <button role="radio" data-action="appearance.page-sleep" data-value="on" aria-checked={mode === "on"} onClick={() => setPageSleep("on")}>{t("settings.on_after_15_minutes")}</button>
      </div>
    </Group>
  );
}
