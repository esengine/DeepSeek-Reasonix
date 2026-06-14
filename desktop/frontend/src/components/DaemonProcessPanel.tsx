import { useCallback, useEffect, useState } from "react";
import { app } from "../lib/bridge";
import { useT } from "../lib/i18n";
import type { DaemonStartupHelperView, DaemonStatusView } from "../lib/types";
import { PromptBadge, PromptDetailToggle, PromptShelf } from "./PromptShelf";

const DAEMON_ADDR = "";
const REFRESH_MS = 10000;

function statusLabel(status: DaemonStatusView | null, t: ReturnType<typeof useT>): string {
  if (status?.connected) return status.status || t("daemonProcess.running");
  return t("daemonProcess.offline");
}

function detailRows(status: DaemonStatusView | null, helper: DaemonStartupHelperView | null, t: ReturnType<typeof useT>) {
  return [
    { label: t("daemonProcess.pid"), value: status?.pid ? String(status.pid) : "" },
    { label: t("daemonProcess.uptime"), value: status?.uptime },
    { label: t("daemonProcess.addr"), value: status?.addr },
    { label: t("daemonProcess.sessions"), value: status?.sessions !== undefined ? String(status.sessions) : "" },
    { label: t("daemonProcess.startup"), value: helper?.platform },
    { label: t("daemonProcess.lastError"), value: status?.error },
  ].filter((row) => row.value);
}

export function DaemonProcessPanel() {
  const t = useT();
  const [status, setStatus] = useState<DaemonStatusView | null>(null);
  const [helper, setHelper] = useState<DaemonStartupHelperView | null>(null);
  const [open, setOpen] = useState(true);
  const [working, setWorking] = useState("");
  const [error, setError] = useState("");
  const [message, setMessage] = useState("");

  const refresh = useCallback(async () => {
    const [nextStatus, nextHelper] = await Promise.all([
      app.DaemonStatus(DAEMON_ADDR),
      app.DaemonStartupHelper(),
    ]);
    setStatus(nextStatus);
    setHelper(nextHelper);
  }, []);

  useEffect(() => {
    void refresh().catch((err) => setError(err instanceof Error ? err.message : String(err)));
    const timer = window.setInterval(() => void refresh().catch((err) => setError(err instanceof Error ? err.message : String(err))), REFRESH_MS);
    return () => window.clearInterval(timer);
  }, [refresh]);

  const runAction = async (key: string, action: () => Promise<{ message?: string; status: DaemonStatusView }>) => {
    if (working) return;
    setWorking(key);
    setError("");
    setMessage("");
    try {
      const result = await action();
      setStatus(result.status);
      setMessage(result.message || "");
      await refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
      await refresh().catch(() => undefined);
    } finally {
      setWorking("");
    }
  };

  const connected = Boolean(status?.connected);
  const rows = detailRows(status, helper, t);
  const meta = [message, error || status?.error].filter(Boolean).join(" · ");
  const badges = (
    <>
      <PromptBadge>{connected ? t("daemonProcess.connected") : t("daemonProcess.offline")}</PromptBadge>
      {status?.sessions !== undefined && <PromptBadge>{t("daemonProcess.sessionCount", { count: status.sessions })}</PromptBadge>}
    </>
  );

  return (
    <PromptShelf
      titleId="daemon-process-title"
      title={t("daemonProcess.title")}
      badges={badges}
      meta={meta || statusLabel(status, t)}
      actionsWrap
      actions={
        <>
          <button className="prompt-action prompt-action--selected" disabled={Boolean(working) || connected} onClick={() => void runAction("start", () => app.StartDaemon(DAEMON_ADDR))}>
            <span className="prompt-action__label">{t("daemonProcess.start")}</span>
          </button>
          <button className="prompt-action" disabled={Boolean(working) || !connected} onClick={() => void runAction("restart", () => app.RestartDaemon(DAEMON_ADDR))}>
            <span className="prompt-action__label">{t("daemonProcess.restart")}</span>
          </button>
          <button className="prompt-action" disabled={Boolean(working) || !connected} onClick={() => void runAction("stop", () => app.StopDaemon(DAEMON_ADDR))}>
            <span className="prompt-action__label">{t("daemonProcess.stop")}</span>
          </button>
          <PromptDetailToggle open={open} label={t("daemonProcess.show")} openLabel={t("daemonProcess.hide")} onClick={() => setOpen((current) => !current)} />
        </>
      }
    >
      {open && (
        <div className="daemon-process__panel">
          <div className="daemon-process__details">
            {rows.map((row) => (
              <div className="daemon-process__detail" key={row.label} title={row.value}>
                <span>{row.label}</span>
                <b>{row.value}</b>
              </div>
            ))}
          </div>
          {helper && (
            <div className="daemon-process__startup">
              <span>{t("daemonProcess.startupDescription")}</span>
              <code>{helper.installCommand}</code>
              <code>{helper.uninstallCommand}</code>
              <code>{helper.printCommand}</code>
            </div>
          )}
        </div>
      )}
    </PromptShelf>
  );
}
