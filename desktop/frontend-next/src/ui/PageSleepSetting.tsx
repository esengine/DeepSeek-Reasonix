import { t } from "../i18n";
import { setPageSleep, usePageSleep } from "../state/pagesleep";
import { Group } from "./Group";

export function PageSleepSetting() {
  const mode = usePageSleep();
  return (
    <Group id="page-sleep" title={t("空闲网页休眠")} hint={t("一段时间没有驱动的网页会被收起以省内存，需要时自动恢复。默认关闭。")}>
      <div className="seg" data-text role="radiogroup" aria-label={t("空闲网页休眠")}>
        <button role="radio" data-action="appearance.page-sleep" data-value="off" aria-checked={mode === "off"} onClick={() => setPageSleep("off")}>{t("保持打开")}</button>
        <button role="radio" data-action="appearance.page-sleep" data-value="on" aria-checked={mode === "on"} onClick={() => setPageSleep("on")}>{t("开启（15 分钟后）")}</button>
      </div>
    </Group>
  );
}
