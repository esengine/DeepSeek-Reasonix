import { useState } from "react";
import { useT } from "../lib/i18n";

export function RemoteReclaimBanner({
  tabId,
  busyTabId,
  onReclaim,
}: {
  tabId: string;
  busyTabId: string | null;
  onReclaim: (tabId: string) => void;
}) {
  const t = useT();
  const [armedTabId, setArmedTabId] = useState<string | null>(null);
  const armed = armedTabId === tabId;
  const busy = busyTabId !== null;

  return (
    <div className="banner banner--warning banner--actionable">
      <span className="banner__msg">{t("takeover.remoteBanner")}</span>
      <span className="banner__spacer" />
      <button
        type="button"
        className={`btn btn--small${armed ? " btn--danger" : ""}`}
        disabled={busy}
        title={armed ? t("takeover.reclaimConfirm") : t("takeover.reclaimTitle")}
        onClick={() => {
          if (busy || !tabId) return;
          if (!armed) {
            setArmedTabId(tabId);
            return;
          }
          setArmedTabId(null);
          onReclaim(tabId);
        }}
      >
        {busyTabId === tabId
          ? t("takeover.reclaiming")
          : armed
            ? t("takeover.reclaimConfirmButton")
            : t("takeover.reclaim")}
      </button>
    </div>
  );
}
