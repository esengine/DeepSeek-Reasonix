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
    void port.openExternal(url).then(() => setFailed(""), () => setFailed(t("无法打开浏览器，请手动访问 {at}", { at: url })));

  return (
    <div className="spon">
      <p className="spon-note">{t("如果 Reasonix 帮你省了时间或 token，欢迎请杯咖啡。这是一杯咖啡，不是合同：捐助不会换来功能优先级，也不会改变问题的处理顺序。")}</p>
      <div className="spon-acts">
        <a className="spon-link" href={COMMUNITY.paypal} data-action="sponsor.paypal" onClick={(e) => { e.preventDefault(); open(COMMUNITY.paypal); }}>PayPal</a>
        <button type="button" className="btn sm" data-action="sponsor.qr" aria-expanded={qr} aria-controls="spon-qr" onClick={() => setQr((v) => !v)}>
          {qr ? t("收起收款码") : t("显示收款码")}
        </button>
      </div>
      {qr && (
        <div id="spon-qr" className="spon-qr">
          <figure>
            <img src={wechatSrc} alt={t("微信支付收款码")} width={180} />
            <figcaption>{t("微信支付")}</figcaption>
          </figure>
          <figure>
            <img src={alipaySrc} alt={t("支付宝收款码")} width={180} />
            <figcaption>{t("支付宝")}</figcaption>
          </figure>
        </div>
      )}
      {failed && <p className="comm-fail" role="alert">{failed}</p>}
    </div>
  );
}
