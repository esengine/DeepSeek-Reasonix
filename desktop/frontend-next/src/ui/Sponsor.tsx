import { useState } from "react";
import { t } from "../i18n";
import type { AgentPort } from "../port/port";
import wechatSrc from "../assets/wechat-pay.jpg";
import alipaySrc from "../assets/alipay.jpg";
import { COMMUNITY } from "./communityLinks";

export function Sponsor({ port }: { port: Pick<AgentPort, "openExternal"> }) {
  const [qr, setQr] = useState(false);
  const [failed, setFailed] = useState("");

  const open = (url: string) =>
    void port.openExternal(url).then(() => setFailed(""), () => setFailed(t("common.could_not_open_a_browser", { at: url })));

  return (
    <div className="spon">
      <p className="spon-note">{t("feedback.if_reasonix_has_saved_you")}</p>
      <div className="spon-acts">
        <a className="spon-link" href={COMMUNITY.paypal} data-action="sponsor.paypal" onClick={(e) => { e.preventDefault(); open(COMMUNITY.paypal); }}>PayPal</a>
        <button type="button" className="btn sm" data-action="sponsor.qr" aria-expanded={qr} aria-controls={qr ? "spon-qr" : undefined} onClick={() => setQr((v) => !v)}>
          {qr ? t("feedback.hide_payment_qr_codes") : t("feedback.show_payment_qr_codes")}
        </button>
      </div>
      {qr && (
        <div id="spon-qr" className="spon-qr">
          <figure>
            <img src={wechatSrc} alt={t("feedback.wechat_pay_qr_code")} width={180} />
            <figcaption>{t("feedback.wechat_pay")}</figcaption>
          </figure>
          <figure>
            <img src={alipaySrc} alt={t("feedback.alipay_qr_code")} width={180} />
            <figcaption>{t("feedback.alipay")}</figcaption>
          </figure>
        </div>
      )}
      {failed && <p className="comm-fail" role="alert">{failed}</p>}
    </div>
  );
}
