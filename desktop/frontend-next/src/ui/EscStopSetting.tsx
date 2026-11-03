import { t } from "../i18n";
import { setEscStop, useEscStop } from "../state/escstop";
import { Group } from "./Group";

export function EscStopSetting() {
  const mode = useEscStop();
  return (
    <Group id="esc-stop" title={t("common.escStopsATask")} hint={t("escStopSetting.whatEscapeDoesWhileA")}>
      <div className="seg" data-text role="radiogroup" aria-label={t("common.escStopsATask")}>
        <button role="radio" data-action="appearance.esc-stop" data-value="confirm" aria-checked={mode === "confirm"} onClick={() => setEscStop("confirm")}>{t("escStopSetting.pressEscTwiceToStop")}</button>
        <button role="radio" data-action="appearance.esc-stop" data-value="now" aria-checked={mode === "now"} onClick={() => setEscStop("now")}>{t("escStopSetting.pressEscOnceToStop")}</button>
      </div>
    </Group>
  );
}
