import { t } from "../i18n";
import { setEscStop, useEscStop } from "../state/escstop";
import { Group } from "./Group";

export function EscStopSetting() {
  const mode = useEscStop();
  return (
    <Group id="esc-stop" title={t("safety.esc_stops_a_task")} hint={t("safety.what_escape_does_while_a")}>
      <div className="seg" data-text role="radiogroup" aria-label={t("safety.esc_stops_a_task")}>
        <button role="radio" data-action="appearance.esc-stop" data-value="confirm" aria-checked={mode === "confirm"} onClick={() => setEscStop("confirm")}>{t("safety.press_esc_twice_to_stop")}</button>
        <button role="radio" data-action="appearance.esc-stop" data-value="now" aria-checked={mode === "now"} onClick={() => setEscStop("now")}>{t("safety.press_esc_once_to_stop")}</button>
      </div>
    </Group>
  );
}
