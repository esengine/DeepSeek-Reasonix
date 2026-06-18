import { useEffect, useState } from "react";
import { useT } from "../lib/i18n";
import { useUpdater } from "../lib/useUpdater";

const MB = 1024 * 1024;
const mb = (n: number) => (n / MB).toFixed(1);

// UpdateBanner checks for an update once on mount and, when one is available, shows
// a dismissible top banner that drives the download → verify → restart/install flow
// (or, on macOS manual builds, links out to the download page). It renders nothing while idle, checking,
// or already current — a quiet auto-check that only surfaces when actionable. A
// failed check can be dismissed here (network blips shouldn't pin the UI); the
// Settings panel is where a manual check shows errors inline.
export function UpdateBanner({ enabled = true }: { enabled?: boolean }) {
  const t = useT();
  const { status, check, download, install, reset } = useUpdater();
  const [dismissed, setDismissed] = useState<string | null>(null);

  useEffect(() => {
    if (!enabled) return;
    void check();
  }, [check, enabled]);

  if (!enabled) return null;

  switch (status.kind) {
    case "available": {
      const info = status.info;
      if (info.latest === dismissed) return null;
      return (
        <div className="banner banner--update">
          <span className="banner__msg">{t("updater.available", { v: info.latest })}</span>
          {!info.canSelfUpdate && <span className="banner__hint">{info.manualReason || t("updater.macHint")}</span>}
          <span className="banner__spacer" />
          <button className="btn btn--small btn--primary" onClick={() => download(info)}>
            {info.canSelfUpdate ? t("updater.downloadUpdate") : t("updater.goToDownload")}
          </button>
          <button className="btn btn--small" onClick={() => setDismissed(info.latest)}>
            {t("updater.dismiss")}
          </button>
        </div>
      );
    }
    case "downloading": {
      const pct = status.total > 0 ? Math.round((status.received / status.total) * 100) : 0;
      return (
        <div className="banner banner--update">
          <span className="banner__msg">
            {t("updater.downloading", { done: mb(status.received), total: mb(status.total), pct })}
          </span>
          <span className="banner__spacer" />
          <progress className="banner__progress" value={status.received} max={status.total || undefined} />
        </div>
      );
    }
    case "verifying":
      return <div className="banner banner--update">{t("updater.verifying")}</div>;
    case "downloaded":
      return (
        <div className="banner banner--update">
          <span className="banner__msg">{t("updater.downloaded", { v: status.info.latest })}</span>
          <span className="banner__spacer" />
          <button className="btn btn--small btn--primary" onClick={install}>
            {t("updater.restartInstall")}
          </button>
          <button className="btn btn--small" onClick={reset}>
            {t("updater.dismiss")}
          </button>
        </div>
      );
    case "installing":
      return <div className="banner banner--update">{t("updater.installing")}</div>;
    case "done":
      return <div className="banner banner--update">{t("updater.done")}</div>;
    case "error":
      const failedMessage = t("updater.failed", { msg: status.message });
      return (
        <div className="banner banner--update banner--error banner--actionable">
          <span className="banner__msg" title={failedMessage}>
            {failedMessage}
          </span>
          <span className="banner__spacer" />
          <button className="btn btn--small btn--primary" onClick={() => void check()}>
            {t("updater.retry")}
          </button>
          <button className="btn btn--small" onClick={reset}>
            {t("updater.dismiss")}
          </button>
        </div>
      );
    default:
      // idle | checking | upToDate — nothing to show.
      return null;
  }
}
