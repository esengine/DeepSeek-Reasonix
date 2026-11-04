import { t } from "../i18n";

export function StopHint({ armed }: { armed: boolean }) {
  return (
    <div className="stophint" role="status" aria-live="polite" hidden={!armed}>
      {armed ? t("composer.press_esc_again_to_stop") : ""}
    </div>
  );
}
