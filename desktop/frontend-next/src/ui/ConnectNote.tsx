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
      <span className="t">{offer.none ? t("还没有连接模型服务，发不出去") : t("当前模型还没有 API key，发不出去")}</span>
      <button data-action="onboarding.reopen" onClick={offer.open}>{t("去连接")}</button>
    </div>
  );
}
