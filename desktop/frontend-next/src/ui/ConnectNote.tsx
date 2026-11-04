import { t } from "../i18n";
import { StudioIcon } from "./StudioIcon";

export const UNCONFIGURED_REF = "unconfigured/none";

export interface ConnectOffer {
  none: boolean;
  open: () => void;
}

export function ConnectNote({ offer }: { offer: ConnectOffer }) {
  return (
    <div className="rtbar posturenote" data-kind="connect" data-lvl="warn" role="status">
      <StudioIcon name="warning" />
      <span className="t">{offer.none ? t("composer.no_model_service_is_connected") : t("composer.the_selected_model_has_no")}</span>
      <button data-action="onboarding.reopen" onClick={offer.open}>{t("composer.connect")}</button>
    </div>
  );
}
