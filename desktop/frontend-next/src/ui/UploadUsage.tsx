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
      title={t("usage.upload_volume")}
      hint={t("settings.size_of_requests_sent")}
    >
      <p className="note">{t("usage.the_size_of_the_request")}</p>
      {failed && <p className="note" role="alert">{failed}</p>}
      {report && report.requests === 0 && <p className="note">{t("usage.nothing_recorded_yet_counting_starts")}</p>}
      {report && report.requests > 0 && (
        <>
          <div className="lrow"><span className="ds">{t("usage.uploaded_today")}</span><span className="sc">{bytes(today)}</span></div>
          <div className="lrow">
            <span className="ds">{t("usage.uploaded_in_the_last_n", { n: DAYS })}</span>
            <span className="sc">{t("usage.size_in_n_requests_avg", { size: bytes(report.bytes), n: count(report.requests), avg: bytes(Math.round(report.bytes / report.requests)) })}</span>
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
