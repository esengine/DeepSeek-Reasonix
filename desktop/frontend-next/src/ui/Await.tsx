import { useEffect, useState } from "react";
import { decimals } from "../i18n/format";
import { t } from "../i18n";
import type { Waiting } from "../state/session";
import { retryPhase, type RetryState } from "../state/retry_line";
import { useShown } from "./shown";

function lead(retry: RetryState): string {
  switch (retry.cause) {
    case "connection_closed":
      return t("连接被中断");
    case "timeout":
      return t("服务器没有及时响应");
    case "upstream_status":
      return t("服务器返回 HTTP {status}", { status: retry.status ?? 0 });
    case "stream_idle":
      return t("回包停住了");
    case "upstream_error":
      return t("服务器在回包中途报错");
  }
  if (retry.scope === "stream") return t("回包写到一半断了");
  return retry.scope === "headers" ? t("请求没有成功") : t("连接已断开");
}

function phase(retry: RetryState, elapsedMs: number): string {
  const replay = retry.scope === "stream";
  const p = retryPhase(retry, elapsedMs);
  const vars = { attempt: retry.attempt, max: retry.max };
  if (p.kind === "backoff") {
    return replay ? t("{n}s 后重放 {attempt}/{max}", { ...vars, n: p.nextSecs }) : t("{n}s 后重试 {attempt}/{max}", { ...vars, n: p.nextSecs });
  }
  const secs = decimals(p.secs, 1);
  if (replay) return t("重放 {attempt}/{max} · 等待服务器响应 {secs}s", { ...vars, secs });
  if (retry.timeoutSecs) {
    return t("重试 {attempt}/{max} · 等待服务器响应 {secs}s（超过 {limit}s 视为无响应）", { ...vars, secs, limit: retry.timeoutSecs });
  }
  return t("重试 {attempt}/{max} · 等待服务器响应 {secs}s", { ...vars, secs });
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
        {retry ? `${lead(retry)} · ${phase(retry, elapsed)}` : t("等待回包 {secs}s", { secs: decimals(elapsed / 1000, 1) })}
      </span>
    </div>
  );
}
