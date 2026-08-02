import { useEffect, useState } from "react";
import { AlertTriangle, CheckCircle2, ChevronDown, Loader2, X } from "lucide-react";
import type { AppBindings } from "../lib/bridge";
import type { Translator } from "../lib/i18n";
import type { ConfigRepairView } from "../lib/types";

type ConfigRepairAPI = Pick<AppBindings,
  "ConfigRepairStatus" | "UndoConfigRepair" | "RestoreGlobalConfigSnapshot" | "OpenConfigFile"
>;

const emptyView: ConfigRepairView = {
  outcome: "",
  scope: "",
  path: "",
  detail: "",
  repairedAt: "",
  undoable: false,
  canOpenFile: false,
};

/**
 * Recovery stays one-click on the common path. File editing, raw diagnostics,
 * and undo remain available under Details for exceptional cases.
 */
export function ConfigRepairBanner({ api, t }: { api: ConfigRepairAPI; t: Translator }) {
  const [view, setView] = useState<ConfigRepairView>(emptyView);
  const [dismissed, setDismissed] = useState(false);
  const [busy, setBusy] = useState(false);
  const [failed, setFailed] = useState(false);

  useEffect(() => {
    let cancelled = false;
    api.ConfigRepairStatus()
      .then((value) => {
        if (!cancelled) setView(value?.outcome ? value : emptyView);
      })
      .catch(() => {});
    return () => { cancelled = true; };
  }, [api]);

  useEffect(() => {
    if (view.outcome !== "config_damaged") return;
    let cancelled = false;
    const refresh = () => {
      api.ConfigRepairStatus()
        .then((value) => {
          if (!cancelled) setView(value?.outcome ? value : emptyView);
        })
        .catch(() => {});
    };
    const timer = window.setInterval(refresh, 2000);
    window.addEventListener("focus", refresh);
    return () => {
      cancelled = true;
      window.clearInterval(timer);
      window.removeEventListener("focus", refresh);
    };
  }, [api, view.outcome]);

  if (!view.outcome || dismissed) return null;
  const damaged = view.outcome === "config_damaged";

  const onUndo = async () => {
    const transactionID = view.transactionId?.trim();
    if (!transactionID) {
      setFailed(true);
      return;
    }
    setBusy(true);
    setFailed(false);
    try {
      await api.UndoConfigRepair(transactionID);
      setDismissed(true);
    } catch {
      setBusy(false);
      setFailed(true);
    }
  };
  const onRestore = async () => {
    setBusy(true);
    setFailed(false);
    try {
      if (await api.RestoreGlobalConfigSnapshot()) setView(emptyView);
      else throw new Error("restore unavailable");
    } catch {
      setBusy(false);
      setFailed(true);
    }
  };

  return (
    <div className={`banner banner--actionable config-repair-banner ${damaged ? "banner--warning" : "banner--success"}`} role={damaged ? "alert" : "status"}>
      {damaged ? <AlertTriangle size={16} aria-hidden /> : <CheckCircle2 size={16} aria-hidden />}
      <span className="banner__msg">
        {damaged ? t("configRepair.damaged") : t("configRepair.completed")}
        {failed && <span className="banner__sub">{t("configRepair.actionFailed")}</span>}
      </span>
      <span className="banner__spacer" />
      {damaged && (
        <button type="button" className="btn btn--small btn--primary" disabled={busy} onClick={() => void onRestore()}>
          {busy && <Loader2 className="spin" size={12} aria-hidden />}
          {busy ? t("configRepair.restoring") : t("configRepair.restore")}
        </button>
      )}
      <details className="banner__more">
        <summary>
          {t("configRepair.details")}
          <ChevronDown size={12} aria-hidden />
        </summary>
        <div className="banner__more-actions">
          <small>{view.detail}</small>
          {view.canOpenFile && (
            <button type="button" className="btn btn--small" disabled={busy} onClick={() => void api.OpenConfigFile().catch(() => setFailed(true))}>
              {t("configRepair.openFile")}
            </button>
          )}
          {view.undoable && (
            <button type="button" className="btn btn--small" disabled={busy} onClick={() => void onUndo()}>
              {t("configRepair.undo")}
            </button>
          )}
        </div>
      </details>
      {!damaged && (
        <button type="button" className="modal-close-button" onClick={() => setDismissed(true)} aria-label={t("common.close")}>
          <X size={14} aria-hidden />
        </button>
      )}
    </div>
  );
}
