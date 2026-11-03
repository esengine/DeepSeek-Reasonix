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
    void port.openExternal(url).then(() => setFailed(""), () => setFailed(t("common.couldNotOpenABrowser", { at: url })));

  return (
    <div className="spon">
      <p className="spon-note">{t("sponsor.ifReasonixHasSavedYou")}</p>
      <div className="spon-acts">
        <a className="spon-link" href={COMMUNITY.paypal} data-action="sponsor.paypal" onClick={(e) => { e.preventDefault(); open(COMMUNITY.paypal); }}>PayPal</a>
        <button type="button" className="btn sm" data-action="sponsor.qr" aria-expanded={qr} aria-controls={qr ? "spon-qr" : undefined} onClick={() => setQr((v) => !v)}>
          {qr ? t("sponsor.hidePaymentQRCodes") : t("sponsor.showPaymentQRCodes")}
        </button>
      </div>
      {qr && (
        <div id="spon-qr" className="spon-qr">
          <figure>
            <img src={wechatSrc} alt={t("sponsor.weChatPayQRCode")} width={180} />
            <figcaption>{t("common.weChatPay")}</figcaption>
          </figure>
          <figure>
            <img src={alipaySrc} alt={t("sponsor.alipayQRCode")} width={180} />
            <figcaption>{t("common.alipay")}</figcaption>
          </figure>
        </div>
      )}
      {failed && <p className="comm-fail" role="alert">{failed}</p>}
    </div>
  );
}
