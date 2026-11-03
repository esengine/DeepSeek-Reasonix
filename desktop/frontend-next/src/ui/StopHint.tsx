import { t } from "../i18n";

export function StopHint({ armed }: { armed: boolean }) {
  return (
    <div className="stophint" role="status" aria-live="polite" hidden={!armed}>
      {armed ? t("stopHint.pressEscAgainToStop") : ""}
    </div>
  );
}
