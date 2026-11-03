import { useEffect, useState } from "react";
import { decimals } from "../i18n/format";
import { t } from "../i18n";
import type { Waiting } from "../state/session";
import { retryPhase, type RetryState } from "../state/retry_line";
import { useShown } from "./shown";

function lead(retry: RetryState): string {
  switch (retry.cause) {
    case "connection_closed":
      return t("await.connectionClosed");
    case "timeout":
      return t("await.theServerDidNotAnswer");
    case "upstream_status":
      return t("await.serverReturnedHTTP", { status: retry.status ?? 0 });
    case "stream_idle":
      return t("await.theResponseStalled");
    case "upstream_error":
      return t("await.theServerReportedAnError");
  }
  if (retry.scope === "stream") return t("await.theResponseWasCutMid");
  return retry.scope === "headers" ? t("await.theRequestDidNotSucceed") : t("await.connectionDropped");
}

function phase(retry: RetryState, elapsedMs: number): string {
  const replay = retry.scope === "stream";
  const p = retryPhase(retry, elapsedMs);
  // A maximum of 0 is the kernel saying the user removed the limit.
  const vars = { attempt: retry.attempt, max: retry.max > 0 ? retry.max : "∞" };
  if (p.kind === "backoff") {
    return replay ? t("await.nextReplayInS", { ...vars, n: p.nextSecs }) : t("await.nextRetryInS", { ...vars, n: p.nextSecs });
  }
  const secs = decimals(p.secs, 1);
  if (replay) return t("await.replayWaitingForTheServer", { ...vars, secs });
  if (retry.timeoutSecs) {
    return t("await.retryWaitingForTheServer", { ...vars, secs, limit: retry.timeoutSecs });
  }
  return t("await.retryWaitingForTheServer2", { ...vars, secs });
}

// Counted from the stamp the wait carries, not from mount: the notice of the
// next attempt replaces the stamp, so the clock always means the attempt in flight.
export function Await({ since, retry }: { since: number; retry?: Waiting["retry"] }) {
  const start = retry?.since ?? since;
  const watched = useShown();
  const [elapsed, setElapsed] = useState(() => Date.now() - start);
  useEffect(() => {
    const tick = () => setElapsed(Date.now() - start);
    tick();
    if (!watched) return;
    const id = setInterval(tick, 100);
    return () => clearInterval(id);
  }, [start, watched]);
  return (
    <div className="await" data-retry={retry ? "" : undefined}>
      <i />
      <i />
      <i />
      <span className="t">
        {retry ? `${lead(retry)} · ${phase(retry, elapsed)}` : t("await.waitingForAResponseS", { secs: decimals(elapsed / 1000, 1) })}
      </span>
    </div>
  );
}
