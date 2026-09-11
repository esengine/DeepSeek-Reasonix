import { useEffect, useState } from "react";
import { useT } from "../lib/i18n";
import { useUpdater } from "../lib/useUpdater";

const MB = 1024 * 1024;
const UPDATE_REFRESH_INTERVAL_MS = 60 * 60 * 1000;
const mb = (n: number) => (n / MB).toFixed(1);

export function subscribeToUpdateRefresh(
  refresh: () => void,
  intervalMs = UPDATE_REFRESH_INTERVAL_MS,
): () => void {
  const refreshVisible = () => {
    if (document.visibilityState === "visible") refresh();
  };
  const interval = window.setInterval(refreshVisible, intervalMs);
  window.addEventListener("focus", refreshVisible);
  document.addEventListener("visibilitychange", refreshVisible);
  return () => {
    window.clearInterval(interval);
    window.removeEventListener("focus", refreshVisible);
    document.removeEventListener("visibilitychange", refreshVisible);
  };
}

// UpdateBanner checks on mount and while the app remains open and, when one is available,
// shows a dismissible top banner with a single "update and restart" action
// (or, on macOS manual builds, links out to the download page). It renders
// nothing while idle, checking, or already current. A failed check can be
// dismissed here; Settings is where a manual check shows errors inline.
export function UpdateBanner({
  enabled = true,
  onShowReleaseNotes,
}: {
  enabled?: boolean;
  onShowReleaseNotes?: (version: string) => void;
}) {
  const t = useT();
  const { status, check, refresh, apply, openDownload, abandonPending, reset } = useUpdater();
  const [dismissed, setDismissed] = useState<string | null>(null);

  useEffect(() => {
    if (!enabled) return;
    void check();
  }, [check, enabled]);

  useEffect(() => {
    if (!enabled) return;
    return subscribeToUpdateRefresh(() => {
      void refresh();
    });
  }, [enabled, refresh]);

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
          {onShowReleaseNotes && (
            <button className="btn btn--small" onClick={() => onShowReleaseNotes(info.latest)}>
              {t("updater.releaseNotes")}
            </button>
          )}
          <button className="btn btn--small btn--primary" onClick={() => apply(info)}>
            {info.canSelfUpdate ? t("updater.updateAndRestart") : t("updater.goToDownload")}
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
    case "authorizing":
      return <div className="banner banner--update">{t("updater.authorizing")}</div>;
    case "installing":
      return (
        <div className="banner banner--update">
          {status.info?.requiresElevation || status.info?.installMode === "deb"
            ? t("updater.installingPackage")
            : t("updater.installing")}
        </div>
      );
    case "relaunching":
    case "done":
      return <div className="banner banner--update">{t("updater.done")}</div>;
    case "error": {
      const failedMessage = status.disposition === "recovery"
        ? t("updater.recoveryBlocked")
        : status.disposition === "manual"
          ? t("updater.manualUpdateRequired")
          : t("updater.failed", { msg: status.message });
      const downloadFirst = status.disposition !== "retryable";
      return (
        <div className="banner banner--update banner--error banner--actionable">
          <span className="banner__msg" title={failedMessage}>
            {failedMessage}
          </span>
          <span className="banner__spacer" />
          {status.disposition === "recovery" && (
            <button
              className="btn btn--small"
              type="button"
              onClick={() => void abandonPending()}
            >
              {t("updater.discardPrevious")}
            </button>
          )}
          {downloadFirst && (
            <button className="btn btn--small btn--primary" type="button" onClick={openDownload}>
              {t("updater.officialDownload")}
            </button>
          )}
          <button
            className={`btn btn--small${downloadFirst ? "" : " btn--primary"}`}
            type="button"
            onClick={() => {
              if (status.info) apply(status.info);
              else void check();
            }}
          >
            {t("updater.retry")}
          </button>
          <button className="btn btn--small" onClick={() => reset()}>
            {t("updater.dismiss")}
          </button>
        </div>
      );
    }
    default:
      // idle | checking | upToDate — nothing to show.
      return null;
  }
}
