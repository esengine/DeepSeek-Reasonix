import { useEffect, useState } from "react";
import { t } from "../i18n";
import { bytes, count, pct } from "../i18n/format";
import { reason } from "../i18n/kernel";
import type { AgentPort, UploadReport } from "../port/port";
import { Group } from "./Group";

const DAYS = 30;

// What model providers were sent in the last month. Every request carries the
// whole conversation, so this grows with context size times request count, not
// with how much was typed; the numbers are request bodies, before the
// connection's own overhead, and downloads are not counted.
export function UploadUsage({ port }: { port: Pick<AgentPort, "uploadUsage"> }) {
  const [report, setReport] = useState<UploadReport | null>(null);
  const [failed, setFailed] = useState("");
  useEffect(() => {
    let live = true;
    port.uploadUsage(DAYS).then((r) => live && setReport(r), (e) => live && setFailed(reason(e)));
    return () => { live = false; };
  }, [port]);
  const today = report?.daily[report.daily.length - 1]?.bytes ?? 0;
  return (
    <Group id="upload-usage"
      title={t("上传量")}
      hint={t("发给模型服务商的请求内容大小（不含连接本身的开销，也不含下载）。每次请求都会带上完整的对话，所以它取决于上下文有多长、请求有多少次，缓存命中只省费用、不省流量。")}
    >
      {failed && <p className="note" role="alert">{failed}</p>}
      {report && report.requests === 0 && <p className="note">{t("还没有记录：从这个版本起开始统计。")}</p>}
      {report && report.requests > 0 && (
        <>
          <div className="lrow"><span className="ds">{t("今天上传")}</span><span className="sc">{bytes(today)}</span></div>
          <div className="lrow">
            <span className="ds">{t("近 {n} 天上传", { n: DAYS })}</span>
            <span className="sc">{t("{size}，{n} 次请求，平均每次 {avg}", { size: bytes(report.bytes), n: count(report.requests), avg: bytes(Math.round(report.bytes / report.requests)) })}</span>
          </div>
          {report.providers.map((p) => (
            <div className="lrow" key={p.provider}>
              <span className="ds">{p.provider}</span>
              <span className="sc">{t("{size}（{share}）", { size: bytes(p.bytes), share: pct(p.percent / 100) })}</span>
            </div>
          ))}
        </>
      )}
    </Group>
  );
}
