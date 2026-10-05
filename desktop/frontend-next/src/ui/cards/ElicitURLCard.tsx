import { useState } from "react";
import { host } from "../../port/host";
import { t } from "../../i18n";
import { Sym } from "../Sym";
import { answerSource } from "../source";
import { useViewer } from "../../state/viewer";
import type { ElicitProps } from "./ElicitCard";

export function ElicitURLCard({ item, onAnswer }: ElicitProps) {
  const origin = item.ask.origin!;
  const url = externalURL(origin.url!);
  const sealed = item.answered !== undefined;
  const answeredBy = answerSource(item.by, useViewer(), item.said);
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState(false);

  const open = async () => {
    if (sealed || busy || !url) return;
    setFailure(false);
    setBusy(true);
    try {
      await host().openExternal(origin.url!);
    } catch {
      setFailure(true);
    } finally {
      setBusy(false);
    }
  };
  const answer = async (action: string) => {
    if (sealed || busy) return;
    setBusy(true);
    try {
      await onAnswer(item.id, item.ask.id, [{ questionId: item.ask.questions[0].id, selected: [action] }]);
    } finally {
      setBusy(false);
    }
  };
  const outcome = item.answered?.[0]?.[0];

  return (
    <div className="call" data-k="ask" data-prompt={sealed ? "settled" : "pending"}>
      <div className="g"><Sym glyph="?" /><span className="line" /></div>
      <div className="c">
        <div className="hl"><span className="nm">{t("MCP 服务器 {name} 请求外部操作", { name: origin.source })}</span></div>
        <div className="out">
          <div className="ask" data-sealed={sealed ? "" : undefined} aria-busy={busy}>
            <div className="ask-pane" data-on="">
              <div className="ask-hint">{t("请在自己的浏览器中完成操作，完成后再继续。打开页面不会自动确认，也不会把浏览器内容交给 agent。")}</div>
              {origin.message && <div className="ask-q">{origin.message}</div>}
              {url && <div className="ask-q"><b>{origin.urlHost || url.hostname}</b></div>}
              <div className="ask-hint" style={{ overflowWrap: "anywhere" }}>{origin.url}</div>
              {url && (url.protocol !== "https:" || url.hostname.includes("xn--")) && <div className="ask-hint" role="note">{t("请核对完整地址：此链接使用 HTTP 或国际化域名。")}</div>}
              {origin.urlLocal && <div className="ask-hint" role="note">{t("此链接指向本机或私有网络。打开前请确认你信任该服务器和目标地址。")}</div>}
              {(failure || !url) && <div className="ask-hint" role="alert">{t("无法打开浏览器。请重试，或手动打开上面的地址。")}</div>}
            </div>
            {sealed ? (
              <div className="ask-done"><b>{item.answeredElsewhere ? answeredBy || t("已在其他窗口处理，请以最新运行状态为准。") : outcome === "accept" ? t("已确认完成") : outcome === "cancel" ? t("已取消") : t("已拒绝提供")}</b></div>
            ) : (
              <div className="ask-foot">
                <button className="dismiss" data-action="ask.answer" data-value="cancel" disabled={busy} onClick={() => void answer("cancel")}>{t("取消")}</button>
                <button className="dismiss" data-action="ask.answer" data-value="decline" disabled={busy} onClick={() => void answer("decline")}>{t("拒绝提供")}</button>
                <button className="btn" data-action="external.open" disabled={busy || !url} onClick={() => void open()}>{t("打开浏览器")}</button>
                <button className="btn" data-primary data-action="ask.answer" data-value="accept" disabled={busy} onClick={() => void answer("accept")}>{t("已完成，继续")}</button>
              </div>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}

function externalURL(raw: string): URL | null {
  try {
    const url = new URL(raw);
    return (url.protocol === "http:" || url.protocol === "https:") && !url.username && !url.password ? url : null;
  } catch {
    return null;
  }
}
