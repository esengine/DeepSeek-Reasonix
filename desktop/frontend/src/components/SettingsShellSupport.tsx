import { CircleAlert, CircleCheck, ExternalLink, RefreshCw } from "lucide-react";
import { openExternal } from "../lib/bridge";
import { useT } from "../lib/i18n";
import { asArray } from "../lib/array";
import type { SandboxView, ShellCapabilityView } from "../lib/types";
import { CopyButton } from "./CopyButton";

const GIT_FOR_WINDOWS_DOWNLOAD_URL = "https://git-scm.com/download/win";

export function gitForWindowsDownloadURL(candidate?: string): string {
  try {
    const parsed = new URL(candidate ?? "");
    if (
      parsed.protocol === "https:"
      && parsed.hostname.toLowerCase() === "git-scm.com"
      && parsed.port === ""
      && parsed.username === ""
      && parsed.password === ""
      && parsed.pathname === "/download/win"
      && parsed.search === ""
      && parsed.hash === ""
    ) {
      return parsed.href;
    }
  } catch {
    // Fall through to the trusted built-in target.
  }
  return GIT_FOR_WINDOWS_DOWNLOAD_URL;
}

// The Sandbox settings section's shell surface: interpreter preference, the
// current session's bound shell vs what a reload would pick, detection, and
// the Windows-only Git for Windows manual repair link. Package managers are
// never launched from this surface; after repairing manually, the user chooses
// when to re-detect and reload the current session.

function effectiveShellLabel(value: string, t: ReturnType<typeof useT>): string {
  switch (value) {
    case "git-bash": return t("settings.effectiveShellGitBash");
    case "pwsh": return t("settings.effectiveShellPwsh");
    case "powershell": return t("settings.effectiveShellPowershell");
    case "bash": return t("settings.effectiveShellBash");
    case "zsh": return t("settings.effectiveShellZsh");
    case "sh": return t("settings.effectiveShellSh");
    case "auto": return t("common.auto");
    default: return value.trim() || t("common.none");
  }
}

function capabilityLabel(id: string, t: ReturnType<typeof useT>): string {
  switch (id) {
    case "git-bash": return t("settings.effectiveShellGitBash");
    case "powershell": return t("settings.effectiveShellPowershell");
    case "pwsh": return t("settings.effectiveShellPwsh");
    case "zsh": return t("settings.shellCapabilityZsh");
    case "sh": return t("settings.shellCapabilitySh");
    case "git": return t("settings.gitCapability");
    default: return t("settings.effectiveShellBash");
  }
}

function RepairCard({ message, guidance, busy, reloadSession }: {
  message: string;
  guidance?: { manager: string; command?: string } | null;
  busy: boolean;
  reloadSession: () => void;
}) {
  const t = useT();
  return (
    <div className="shell-support__card">
      <div className="shell-support__hint">{message}</div>
      {guidance?.command && (
        <>
          <div className="shell-support__repair-command">
            <code>{guidance.command}</code>
            <CopyButton text={guidance.command} className="btn btn--small" label={t("settings.shellCopyCommand")} />
          </div>
          <div className="shell-support__repair-safety">{t("settings.shellRepairCommandHint")}</div>
        </>
      )}
      <div className="shell-support__actions">
        <button type="button" className="btn btn--small" disabled={busy} onClick={reloadSession}>
          <RefreshCw size={13} aria-hidden="true" />
          <span>{t("settings.shellRepairReload")}</span>
        </button>
      </div>
    </div>
  );
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
  reloadSession,
}: {
  sb: SandboxView;
  windows: boolean;
  busy: boolean;
  setShell: (prefer: string) => void;
  reloadSession: () => void;
}) {
  const t = useT();
  const capabilities = asArray(sb.shellCapabilities);
  const gitBash = capabilities.find((cap) => cap.id === "git-bash");
  const git = sb.gitCapability ?? null;
  const bashMissing = windows ? !gitBash?.available : !capabilities.some((cap) => cap.id === "bash" && cap.available);
  const action = windows ? sb.shellInstallAction ?? null : null;
  const guidance = windows ? null : sb.shellRepairGuidance ?? null;
  const gitGuidance = windows ? null : sb.gitRepairGuidance ?? null;
  const nativeFallback = sb.resolvedShell === "zsh" || sb.resolvedShell === "sh";
  // The manual download entry exists only on Windows; macOS and Linux use
  // platform-native detect-and-guide cards.
  const showInstallCard = windows && action != null && !gitBash?.available;

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
            {git && <DetectionRow cap={git} t={t} />}
          </div>
          {showInstallCard && (
            <div className="shell-support__card">
              <div className="shell-support__hint">{t("settings.shellInstallManualNotice")}</div>
              <div className="shell-support__actions">
                <button type="button" className="btn btn--small" onClick={() => void openExternal(gitForWindowsDownloadURL(action.manualUrl))}>
                  <ExternalLink size={14} aria-hidden="true" />
                  <span>{t("settings.shellInstallManualLink")}</span>
                </button>
                <button type="button" className="btn btn--small" disabled={busy} onClick={reloadSession}>
                  <RefreshCw size={13} aria-hidden="true" />
                  <span>{t("settings.shellRepairReload")}</span>
                </button>
              </div>
            </div>
          )}
          {!windows && bashMissing && !nativeFallback && (
            <RepairCard message={t("settings.shellBashManualRepair")} guidance={guidance} busy={busy} reloadSession={reloadSession} />
          )}
          {!windows && git && !git.available && (
            <RepairCard message={t("settings.gitManualRepair")} guidance={gitGuidance} busy={busy} reloadSession={reloadSession} />
          )}
        </div>, true)}
    </>
  );
}
