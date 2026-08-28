import { useEffect, useRef, useState } from "react";
import { CircleAlert, CircleCheck, ExternalLink, Loader2, RefreshCw } from "lucide-react";
import { app, openExternal } from "../lib/bridge";
import { useT } from "../lib/i18n";
import { asArray } from "../lib/array";
import type { SandboxView, ShellCapabilityView } from "../lib/types";

// The Sandbox settings section's shell surface: interpreter preference, the
// current session's bound shell vs what a reload would pick, detection, and
// the Windows-only Git for Windows helper install. An install never touches
// the preference or the live session — after success the reload button is the
// user's explicit switch, and a user who picks PowerShell mid-install keeps
// that choice because late results only update the note below.

type InstallTone = "ok" | "warn" | "error";

type InstallNote = { tone: InstallTone; text: string; manualUrl?: string };

function effectiveShellLabel(value: string, t: ReturnType<typeof useT>): string {
  switch (value) {
    case "git-bash": return t("settings.effectiveShellGitBash");
    case "pwsh": return t("settings.effectiveShellPwsh");
    case "powershell": return t("settings.effectiveShellPowershell");
    case "bash": return t("settings.effectiveShellBash");
    case "auto": return t("common.auto");
    default: return value.trim() || t("common.none");
  }
}

function capabilityLabel(id: string, t: ReturnType<typeof useT>): string {
  switch (id) {
    case "git-bash": return t("settings.effectiveShellGitBash");
    case "powershell": return t("settings.effectiveShellPowershell");
    case "pwsh": return t("settings.effectiveShellPwsh");
    default: return t("settings.effectiveShellBash");
  }
}

function installNote(status: string, reason: string | undefined, manualUrl: string | undefined, t: ReturnType<typeof useT>): InstallNote {
  switch (status) {
    case "installed": return { tone: "ok", text: t("settings.shellInstallInstalled") };
    case "already_available": return { tone: "ok", text: t("settings.shellInstallAlreadyAvailable") };
    case "cancelled": return { tone: "warn", text: t("settings.shellInstallCancelled") };
    case "busy": return { tone: "warn", text: t("settings.shellInstallBusy") };
    case "manual_required": return { tone: "warn", text: t("settings.shellInstallManualRequired"), manualUrl: manualUrl || "https://git-scm.com/download/win" };
    case "failed": return { tone: "error", text: reason ? t("settings.shellInstallFailedReason", { reason }) : t("settings.shellInstallFailedGeneric"), manualUrl };
    default: return { tone: "warn", text: reason || status };
  }
}

function field(label: string, control: React.ReactNode, stacked = false) {
  return (
    <div className={`settings-field${stacked ? " settings-field--stacked" : ""}`}>
      <div className="settings-field__copy">
        <div className="settings-field__copy-body">
          <div className="settings-field__label">{label}</div>
        </div>
      </div>
      <div className="settings-field__control">{control}</div>
    </div>
  );
}

function DetectionRow({ cap, t }: { cap: ShellCapabilityView; t: ReturnType<typeof useT> }) {
  return (
    <div className="shell-capability__row">
      {cap.available ? <CircleCheck size={14} aria-hidden="true" /> : <CircleAlert size={14} aria-hidden="true" />}
      <span className="shell-capability__name">{capabilityLabel(cap.id, t)}</span>
      <span className="shell-capability__detail">
        {cap.available ? (cap.path ? t("settings.shellDetectedAt", { path: cap.path }) : t("settings.shellDetected")) : t("settings.shellNotDetected")}
      </span>
    </div>
  );
}

export function ShellInterpreterFields({
  sb,
  windows,
  busy,
  setShell,
  refresh,
  reloadSession,
}: {
  sb: SandboxView;
  windows: boolean;
  busy: boolean;
  setShell: (prefer: string) => void;
  refresh: () => Promise<unknown>;
  reloadSession: () => void;
}) {
  const t = useT();
  const [installing, setInstalling] = useState(false);
  const [note, setNote] = useState<InstallNote | null>(null);
  // requestId + mounted guard: only the newest install's result may land, and
  // none may land after the settings page unmounts.
  const requestId = useRef(0);
  const mounted = useRef(true);
  useEffect(() => {
    mounted.current = true;
    return () => { mounted.current = false; };
  }, []);

  const capabilities = asArray(sb.shellCapabilities);
  const gitBash = capabilities.find((cap) => cap.id === "git-bash");
  const bashMissing = windows ? !gitBash?.available : !capabilities.some((cap) => cap.id === "bash" && cap.available);
  const action = windows ? sb.shellInstallAction ?? null : null;
  // The install entry exists only on Windows; macOS and Linux detect and guide.
  const showInstallCard = windows && action != null && !gitBash?.available;

  const beginInstall = async () => {
    if (installing || !action) return;
    const id = ++requestId.current;
    setInstalling(true);
    setNote(null);
    try {
      const res = await app.InstallShellSupport(action.id);
      if (!mounted.current || id !== requestId.current) return;
      // Refresh detection only. The preference and the live session's shell
      // stay untouched; the reload button above is the user's explicit switch.
      if (res.status === "installed" || res.status === "already_available") {
        await refresh();
        if (!mounted.current || id !== requestId.current) return;
      }
      setNote(installNote(res.status, res.reason, res.manualUrl, t));
    } catch (err: any) {
      if (!mounted.current || id !== requestId.current) return;
      setNote({ tone: "error", text: err?.message || String(err) });
    } finally {
      if (mounted.current && id === requestId.current) setInstalling(false);
    }
  };

  return (
    <>
      {field(t("settings.shellInterpreter"),
        <select className="mem-select set-grow" value={sb.shell || "auto"} disabled={busy} onChange={(e) => setShell(e.target.value)}>
          <option value="auto">{windows ? t("settings.shellAutoWindows") : t("settings.shellAuto")}</option>
          <option value="bash">{t("settings.shellBash")}</option>
          <option value="powershell">{t("settings.shellPowershell")}</option>
          <option value="pwsh">{t("settings.shellPwsh")}</option>
        </select>)}
      {field(t("settings.effectiveShell"),
        <div className="settings-readonly-field">{effectiveShellLabel(String(sb.effectiveShell || sb.shell || ""), t)}</div>)}
      {field(t("settings.resolvedShell"),
        <div className="settings-readonly-field">
          {effectiveShellLabel(String(sb.resolvedShell || sb.shell || ""), t)}
          {sb.shellReloadRequired && (
            <button type="button" className="btn btn--small set-shell-reload" disabled={busy} onClick={reloadSession}>
              <RefreshCw size={13} aria-hidden="true" />
              <span>{t("settings.shellReloadNow")}</span>
            </button>
          )}
        </div>)}
      {field(t("settings.shellDetection"),
        <div className="shell-support">
          <div className="settings-readonly-field shell-support__detection" aria-label={t("settings.shellDetection")}>
            {capabilities.map((cap) => <DetectionRow key={cap.id} cap={cap} t={t} />)}
          </div>
          {showInstallCard && (
            <div className="shell-support__card">
              {action.mode === "winget-user" ? (
                <>
                  <div className="shell-support__hint">{t("settings.shellInstallNotice")}</div>
                  <div className="shell-support__actions">
                    <button type="button" className="btn btn--small btn--primary" disabled={installing} onClick={() => void beginInstall()}>
                      {installing
                        ? (<><Loader2 size={14} className="spin" aria-hidden="true" /><span>{t("settings.shellInstalling")}</span></>)
                        : <span>{t("settings.shellInstallAction")}</span>}
                    </button>
                    {installing && (
                      <button type="button" className="btn btn--small" onClick={() => void app.CancelShellInstall()}>
                        <span>{t("settings.shellInstallCancel")}</span>
                      </button>
                    )}
                  </div>
                </>
              ) : (
                <>
                  <div className="shell-support__hint">{t("settings.shellInstallManualNotice")}</div>
                  <div className="shell-support__actions">
                    <button type="button" className="btn btn--small" onClick={() => void openExternal(action.manualUrl || "https://git-scm.com/download/win")}>
                      <ExternalLink size={14} aria-hidden="true" />
                      <span>{t("settings.shellInstallManualLink")}</span>
                    </button>
                  </div>
                </>
              )}
            </div>
          )}
          {!windows && bashMissing && (
            <div className="shell-support__card">
              <div className="shell-support__hint">{t("settings.shellBashManualRepair")}</div>
            </div>
          )}
          {note && (
            <div className={`shell-support__note shell-support__note--${note.tone}`} role="status">
              <span>{note.text}</span>
              {note.manualUrl && (
                <button type="button" className="btn btn--small" onClick={() => void openExternal(note.manualUrl as string)}>
                  <ExternalLink size={13} aria-hidden="true" />
                  <span>{t("settings.shellInstallManualLink")}</span>
                </button>
              )}
            </div>
          )}
        </div>, true)}
    </>
  );
}
