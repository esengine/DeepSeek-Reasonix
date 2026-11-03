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
      title={t("common.uploadVolume")}
      hint={t("uploadUsage.theSizeOfTheRequest")}
    >
      {failed && <p className="note" role="alert">{failed}</p>}
      {report && report.requests === 0 && <p className="note">{t("uploadUsage.nothingRecordedYetCountingStarts")}</p>}
      {report && report.requests > 0 && (
        <>
          <div className="lrow"><span className="ds">{t("uploadUsage.uploadedToday")}</span><span className="sc">{bytes(today)}</span></div>
          <div className="lrow">
            <span className="ds">{t("uploadUsage.uploadedInTheLastDays", { n: DAYS })}</span>
            <span className="sc">{t("uploadUsage.inRequestsEachOnAverage", { size: bytes(report.bytes), n: count(report.requests), avg: bytes(Math.round(report.bytes / report.requests)) })}</span>
          </div>
          {report.providers.map((p) => (
            <div className="lrow" key={p.provider}>
              <span className="ds">{p.provider}</span>
              <span className="sc">{t("usage.text", { size: bytes(p.bytes), share: pct(p.percent / 100) })}</span>
            </div>
          ))}
        </>
      )}
    </Group>
  );
}
