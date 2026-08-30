import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { Check, ChevronDown, FileText, Folder, Plus } from "lucide-react";
import { app } from "../lib/bridge";
import { useT } from "../lib/i18n";
import { useRemoteStore, waitForRemoteConnection } from "../store/remote";
import { RemoteStatusChip } from "./RemoteHostsPage";
import type { RemoteDirEntry, RemoteHostInput, RemoteHostView } from "../lib/types";

type WizardStep = "config" | "connecting" | "workspace";
const STEP_ORDER: WizardStep[] = ["config", "connecting", "workspace"];
const HOST_INPUT_ID = "remote-wizard-host-input";
const HOST_MENU_ID = "remote-wizard-host-menu";

const blankInput: RemoteHostInput = {
  label: "",
  host: "",
  port: 22,
  user: "",
  identityFile: "",
  proxyJump: "",
  defaultWorkspace: "",
  serveInstall: "npm",
  credentialMode: "remote",
  useSSHConfig: false,
};

function formatBytes(n: number): string {
  if (!Number.isFinite(n) || n <= 0) return "";
  const units = ["B", "KiB", "MiB", "GiB"];
  let value = n;
  let unit = 0;
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024;
    unit += 1;
  }
  return `${value >= 100 ? Math.round(value) : value.toFixed(1)} ${units[unit]}`;
}

function parentOf(path: string): string {
  const trimmed = path.replace(/\/+$/, "");
  const idx = trimmed.lastIndexOf("/");
  if (idx <= 0) return "/";
  return trimmed.slice(0, idx);
}

/**
 * RemoteConnectWizard — three-step dialog behind the add-project "remote
 * connection" entry. A non-interactive stepper rail on the left marks
 * progress (number → green check); the right pane hosts the active step:
 *
 *   1. connection config (host suggestions from saved SSH connections,
 *      port, user, auth = password | key file, CLI download method)
 *   2. connecting (ConnectRemoteHost + waitForRemoteConnection; TOFU and
 *      secret prompts surface through the global dialogs)
 *   3. remote workspace picker (SFTP browse + free-text path); finish pins
 *      the canonical workspace and opens its in-app remote session tab.
 */
export function RemoteConnectWizard({
  onClose,
  onRefresh,
  onMerged,
}: {
  onClose: () => void;
  onRefresh: () => Promise<void>;
  onMerged?: (message: string) => void;
}) {
  const t = useT();
  const hosts = useRemoteStore((s) => s.hosts);
  const statuses = useRemoteStore((s) => s.statuses);
  const setHosts = useRemoteStore((s) => s.setHosts);
  const [step, setStep] = useState<WizardStep>("config");
  const [form, setForm] = useState<RemoteHostInput>(blankInput);
  const [authMode, setAuthMode] = useState<"password" | "key">("password");
  const [pickedHostId, setPickedHostId] = useState<string | null>(null);
  const [hostListOpen, setHostListOpen] = useState(false);
  const [hostId, setHostId] = useState("");
  const [connectErr, setConnectErr] = useState("");
  const [startPath, setStartPath] = useState("~");
  const [workspace, setWorkspace] = useState("");
  const [entries, setEntries] = useState<RemoteDirEntry[] | null>(null);
  const [listErr, setListErr] = useState("");

  const [showHidden, setShowHidden] = useState(false);
  const [selectedFile, setSelectedFile] = useState<string | null>(null);
  const [logLines, setLogLines] = useState<Array<{ time: string; level: "info" | "warn"; text: string }>>([]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const dialogRef = useRef<HTMLDivElement>(null);
  const hostInputRef = useRef<HTMLInputElement>(null);
  const hostToggleRef = useRef<HTMLButtonElement>(null);
  const portInputRef = useRef<HTMLInputElement>(null);
  const suggestRef = useRef<HTMLDivElement>(null);
  const pendingHostMenuFocusRef = useRef<"first" | "last" | null>(null);
  const restoreFocusRef = useRef<HTMLElement | null>(null);
  const listRequestRef = useRef(0);
  const host = hosts.find((h) => h.id === hostId) ?? null;
  const pickedHost = pickedHostId ? hosts.find((h) => h.id === pickedHostId) ?? null : null;

  useLayoutEffect(() => {
    restoreFocusRef.current = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    hostInputRef.current?.focus();
    return () => {
      if (restoreFocusRef.current?.isConnected) restoreFocusRef.current.focus();
    };
  }, []);

  useEffect(() => {
    let cancelled = false;
    void app
      .RemoteHosts()
      .then((list) => {
        if (!cancelled) setHosts(list);
      })
      .catch(() => {});
    return () => {
      cancelled = true;
    };
  }, [setHosts]);

  useEffect(() => () => {
    listRequestRef.current += 1;
  }, []);

  useLayoutEffect(() => {
    const edge = pendingHostMenuFocusRef.current;
    if (!hostListOpen || !edge) return;
    pendingHostMenuFocusRef.current = null;
    const items = Array.from(
      suggestRef.current?.querySelectorAll<HTMLButtonElement>('[role="menuitem"]') ?? [],
    );
    (edge === "first" ? items[0] : items[items.length - 1])?.focus();
  }, [hostListOpen, hosts.length]);

  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape" && !busy) {
        event.preventDefault();
        event.stopPropagation();
        // With the saved-host list open, the first Escape closes the list;
        // only the next one exits the wizard.
        if (hostListOpen) {
          setHostListOpen(false);
          hostToggleRef.current?.focus();
          return;
        }
        onClose();
        return;
      }
      if (
        event.key === "Tab" &&
        hostListOpen &&
        document.activeElement instanceof HTMLElement &&
        document.activeElement.getAttribute("role") === "menuitem"
      ) {
        event.preventDefault();
        setHostListOpen(false);
        (event.shiftKey ? hostToggleRef.current : portInputRef.current)?.focus();
        return;
      }
      if (event.key !== "Tab") return;
      const focusable = Array.from(dialogRef.current?.querySelectorAll<HTMLElement>(
        'input:not(:disabled), button:not(:disabled), [tabindex]:not([tabindex="-1"])',
      ) ?? []);
      if (focusable.length === 0) return;
      const first = focusable[0];
      const last = focusable[focusable.length - 1];
      if (!dialogRef.current?.contains(document.activeElement)) {
        event.preventDefault();
        (event.shiftKey ? last : first).focus();
      } else if (event.shiftKey && document.activeElement === first) {
        event.preventDefault();
        last.focus();
      } else if (!event.shiftKey && document.activeElement === last) {
        event.preventDefault();
        first.focus();
      }
    };
    document.addEventListener("keydown", onKey, { capture: true });
    return () => document.removeEventListener("keydown", onKey, { capture: true });
  }, [busy, hostListOpen, onClose]);

  // The saved-host dropdown only closes on explicit dismissal: a pick, the
  // arrow, Escape, or a pointer press outside the host field wrapper.
  useEffect(() => {
    if (!hostListOpen) return;
    const onPointerDown = (event: PointerEvent) => {
      const wrapper = suggestRef.current;
      if (wrapper && event.target && !wrapper.contains(event.target as Node)) {
        setHostListOpen(false);
      }
    };
    document.addEventListener("pointerdown", onPointerDown, true);
    return () => document.removeEventListener("pointerdown", onPointerDown, true);
  }, [hostListOpen]);

  const set = <K extends keyof RemoteHostInput>(key: K, value: RemoteHostInput[K]) =>
    setForm((current) => ({ ...current, [key]: value }));

  const pickSaved = (saved: RemoteHostView) => {
    setForm({
      label: saved.label,
      host: saved.host,
      port: saved.port,
      user: saved.user,
      identityFile: saved.identityFile,
      proxyJump: saved.proxyJump,
      defaultWorkspace: saved.defaultWorkspace,
      serveInstall: saved.serveInstall,
      credentialMode: saved.credentialMode || "remote",
      useSSHConfig: saved.useSSHConfig,
    });
    setAuthMode(saved.identityFile ? "key" : "password");
    setPickedHostId(saved.id);
    setHostListOpen(false);
    hostInputRef.current?.focus();
  };

  const openDir = async (id: string, path: string) => {
    if (!id) return;
    const requestId = ++listRequestRef.current;
    setWorkspace(path);
    setEntries(null);
    setListErr("");
    try {
      const nextEntries = await app.ListRemoteDir(id, path);
      if (requestId !== listRequestRef.current) return;
      setEntries(nextEntries);
    } catch (e) {
      if (requestId !== listRequestRef.current) return;
      setEntries([]);
      setListErr(e instanceof Error ? e.message : String(e));
    }
  };

  const logRef = useRef<HTMLDivElement>(null);

  const pushLog = (level: "info" | "warn", text: string) => {
    const now = new Date();
    const time = [now.getHours(), now.getMinutes(), now.getSeconds()].map((n) => String(n).padStart(2, "0")).join(":");
    setLogLines((current) => [...current, { time, level, text }]);
  };

  useEffect(() => {
    if (logRef.current) logRef.current.scrollTop = logRef.current.scrollHeight;
  }, [logLines]);

  // targetHost comes from the caller: the render closure's `host` still
  // reflects the pre-save hostId on the first connect, which logged the raw
  // host id instead of user@host.
  const connect = async (id: string, startPath: string, targetHost: RemoteHostView | null) => {
    setBusy(true);
    setConnectErr("");
    setLogLines([]);
    setStep("connecting");
    const target = targetHost ? `${targetHost.user ? `${targetHost.user}@` : ""}${targetHost.host}${targetHost.port && targetHost.port !== 22 ? `:${targetHost.port}` : ""}` : id;
    pushLog("info", t("remoteWizard.logConnecting", { target }));
    try {
      await app.ConnectRemoteHost(id);
      pushLog("info", t("remoteWizard.logDetecting"));
      await waitForRemoteConnection(id);
      pushLog("info", t("remoteWizard.logConnected"));
      // Reject unsupported remote OSes (V1: Linux/macOS) before directory
      // browsing; the error keeps the wizard on the connecting step.
      await app.CheckRemotePlatform(id);
      pushLog("info", t("remoteWizard.logPrepare"));
      setStep("workspace");
      void openDir(id, startPath);
    } catch (e) {
      const message = e instanceof Error ? e.message : String(e);
      pushLog("warn", t("remoteWizard.logFailed", { error: message }));
      setConnectErr(message);
    } finally {
      setBusy(false);
    }
  };

  const nextFromConfig = async () => {
    if (busy) return;
    const hostValue = form.host.trim();
    const userValue = form.user.trim();
    const missing: string[] = [];
    if (!hostValue) missing.push(t("remote.host.host"));
    if (!userValue) missing.push(t("remoteWizard.userShort"));
    if (authMode === "password" && !form.password?.trim() && !pickedHost?.passwordSet) {
      missing.push(t("remoteWizard.authPassword"));
    }
    if (authMode === "key" && !form.identityFile.trim()) {
      missing.push(t("remoteWizard.identityFileShort"));
    }
    if (missing.length > 0) {
      setError(t("remoteWizard.required", { fields: missing.join(t("remoteWizard.requiredJoin")) }));
      return;
    }
    setBusy(true);
    setError("");
    try {
      const input: RemoteHostInput = {
        ...form,
        host: hostValue,
        label: form.label.trim() || hostValue,
        password: form.password || undefined,
        keyPassphrase: form.keyPassphrase || undefined,
      };
      const saved = pickedHostId ? await app.UpdateRemoteHost(pickedHostId, input) : await app.AddRemoteHost(input);
      setHosts(await app.RemoteHosts());
      setPickedHostId(saved.id);
      setHostId(saved.id);
      const nextStartPath =
        form.defaultWorkspace.trim() ||
        (await app.RemoteLastWorkspace(saved.id).catch(() => "")) ||
        "~";
      setStartPath(nextStartPath);
      await connect(saved.id, nextStartPath, saved);
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
      setStep("config");
    } finally {
      setBusy(false);
    }
  };



  const finish = async () => {
    const target = workspace.trim();
    if (!hostId || !target || busy) return;
    setBusy(true);
    setError("");
    try {
      let project: Awaited<ReturnType<typeof app.AddRemoteProject>> | null = null;
      try {
        project = await app.AddRemoteProject(hostId, target);
      } catch (e) {
        setError(e instanceof Error ? e.message : String(e));
        return;
      }
      const canonical = project.merged ? project.workspace : target;
      if (project.merged) onMerged?.(t("remoteWizard.mergedProject", { path: canonical }));
      try {
        await app.OpenRemoteProjectTab(hostId, canonical, { newSession: true });
      } catch (e) {
        setError(e instanceof Error ? e.message : String(e));
        if (!project.merged) {
          try {
            await app.RemoveRemoteProject(hostId, target);
          } catch {
            await onRefresh().catch(() => {});
          }
        }
        return;
      }
      await onRefresh();
      onClose();
    } finally {
      setBusy(false);
    }
  };

  const stepIndex = STEP_ORDER.indexOf(step);

  return createPortal(
    <div
      className="modal-backdrop remote-wizard-backdrop"
      role="presentation"
      onMouseDown={(event) => {
        if (!busy && event.target === event.currentTarget) onClose();
      }}
    >
      <div ref={dialogRef} className="modal remote-wizard" role="dialog" aria-modal="true" aria-label={t("remoteWizard.title")}>
        <div className="modal__title">{t("remoteWizard.title")}</div>
        <div className="remote-wizard__frame">
          <div className="remote-wizard__rail" aria-hidden="true">
            {STEP_ORDER.map((name, index) => {
              const done = index < stepIndex;
              const current = name === step;
              return (
                <div
                  key={name}
                  className={`remote-wizard__rail-item${current ? " remote-wizard__rail-item--current" : ""}${done ? " remote-wizard__rail-item--done" : ""}`}
                >
                  <span className="remote-wizard__rail-index">{done ? <Check size={12} aria-hidden="true" /> : index + 1}</span>
                  <span className="remote-wizard__rail-label">
                    {name === "config" ? t("remoteWizard.stepConfig") : name === "connecting" ? t("remoteWizard.stepConnecting") : t("remoteWizard.stepWorkspace")}
                  </span>
                </div>
              );
            })}
          </div>

          <div className="remote-wizard__body">
            {step === "config" ? (
              <>
                <div className="remote-wizard__form">
                <div className="remote-wizard__field-row">
                  <div className="remote-wizard__suggest" ref={suggestRef}>
                    <div className="remote-wizard__field">
                      <label htmlFor={HOST_INPUT_ID}>{t("remote.host.host")}</label>
                      <div className="remote-wizard__host-box">
                        <input
                          id={HOST_INPUT_ID}
                          ref={hostInputRef}
                          value={form.host}
                          disabled={busy}
                          autoComplete="off"
                          placeholder={t("remoteWizard.hostPlaceholder")}
                          onChange={(event) => {
                            set("host", event.target.value);
                            setPickedHostId(null);
                          }}
                        />
                        {hosts.length > 0 ? (
                          <button
                            ref={hostToggleRef}
                            type="button"
                            className={`remote-wizard__suggest-toggle${hostListOpen ? " remote-wizard__suggest-toggle--open" : ""}`}
                            disabled={busy}
                            aria-haspopup="menu"
                            aria-expanded={hostListOpen}
                            aria-controls={HOST_MENU_ID}
                            title={t("remoteWizard.suggestions")}
                            onClick={() => {
                              if (hostListOpen) {
                                pendingHostMenuFocusRef.current = null;
                                setHostListOpen(false);
                                return;
                              }
                              pendingHostMenuFocusRef.current = "first";
                              setHostListOpen(true);
                            }}
                            onKeyDown={(event) => {
                              if (event.key !== "ArrowDown" && event.key !== "ArrowUp") return;
                              event.preventDefault();
                              pendingHostMenuFocusRef.current = event.key === "ArrowDown" ? "first" : "last";
                              if (hostListOpen) {
                                const items = Array.from(
                                  suggestRef.current?.querySelectorAll<HTMLButtonElement>('[role="menuitem"]') ?? [],
                                );
                                pendingHostMenuFocusRef.current = null;
                                (event.key === "ArrowDown" ? items[0] : items[items.length - 1])?.focus();
                              } else {
                                setHostListOpen(true);
                              }
                            }}
                          >
                            <ChevronDown size={14} aria-hidden="true" />
                          </button>
                        ) : null}
                      </div>
                    </div>
                    {hostListOpen && hosts.length > 0 ? (
                      <div
                        className="remote-wizard__suggest-list"
                        role="menu"
                        id={HOST_MENU_ID}
                        aria-label={t("remoteWizard.suggestions")}
                        onKeyDown={(event) => {
                          if (!["ArrowDown", "ArrowUp", "Home", "End"].includes(event.key)) return;
                          event.preventDefault();
                          event.stopPropagation();
                          const items = Array.from(
                            event.currentTarget.querySelectorAll<HTMLButtonElement>('[role="menuitem"]'),
                          );
                          if (items.length === 0) return;
                          const current = items.indexOf(document.activeElement as HTMLButtonElement);
                          const next =
                            event.key === "Home"
                              ? 0
                              : event.key === "End"
                                ? items.length - 1
                                : event.key === "ArrowDown"
                                  ? (current + 1) % items.length
                                  : (current - 1 + items.length) % items.length;
                          items[next]?.focus();
                        }}
                      >
                        <div className="remote-wizard__suggest-head" aria-hidden="true">{t("remoteWizard.suggestions")}</div>
                        {hosts.map((saved) => (
                          <button
                            key={saved.id}
                            type="button"
                            role="menuitem"
                            tabIndex={-1}
                            onClick={() => pickSaved(saved)}
                          >
                            <span className="remote-wizard__suggest-label">{saved.label}</span>
                            <span className="remote-wizard__suggest-detail">
                              {saved.user ? `${saved.user}@` : ""}
                              {saved.host}
                              {saved.port && saved.port !== 22 ? `:${saved.port}` : ""}
                            </span>
                          </button>
                        ))}
                      </div>
                    ) : null}
                  </div>
                  <label className="remote-wizard__field">
                    <span>{t("remote.host.port")}</span>
                    <input
                      ref={portInputRef}
                      type="text"
                      inputMode="numeric"
                      placeholder="22"
                      value={form.port === 0 ? "" : String(form.port)}
                      disabled={busy}
                      onChange={(event) => {
                        const digits = event.target.value.replace(/[^0-9]/g, "").slice(0, 5);
                        set("port", digits ? Number(digits) : 0);
                      }}
                    />
                  </label>
                </div>
                <div className="remote-wizard__field-row">
                  <label className="remote-wizard__field">
                    <span>{t("remote.host.user")}</span>
                    <input value={form.user} disabled={busy} placeholder={t("remoteWizard.userPlaceholder")} onChange={(event) => set("user", event.target.value)} />
                  </label>
                  <div className="remote-wizard__field">
                    <span>{t("remoteWizard.authMethod")}</span>
                    <div className="provider-add-segmented remote-wizard__seg" role="group" aria-label={t("remoteWizard.authMethod")}>
                      <button
                        type="button"
                        className={`provider-add-segmented__item${authMode === "password" ? " provider-add-segmented__item--active" : ""}`}
                        disabled={busy}
                        onClick={() => setAuthMode("password")}
                      >
                        {t("remoteWizard.authPassword")}
                      </button>
                      <button
                        type="button"
                        className={`provider-add-segmented__item${authMode === "key" ? " provider-add-segmented__item--active" : ""}`}
                        disabled={busy}
                        onClick={() => setAuthMode("key")}
                      >
                        {t("remoteWizard.authKey")}
                      </button>
                    </div>
                  </div>
                </div>
                {authMode === "password" ? (
                  <label className="remote-wizard__field">
                    <span>{t("remoteWizard.authPassword")}</span>
                    <input
                      type="password"
                      autoComplete="new-password"
                      placeholder={pickedHost?.passwordSet ? t("remote.host.credentialSavedPlaceholder") : t("remoteWizard.passwordPlaceholder")}
                      value={form.password ?? ""}
                      disabled={busy}
                      onChange={(event) => setForm((current) => ({ ...current, password: event.target.value, clearPassword: false }))}
                    />
                  </label>
                ) : (
                  <>
                    <label className="remote-wizard__field">
                      <span>{t("remote.host.identityFile")}</span>
                      <div className="remote-wizard__identity-row">
                        <input
                          value={form.identityFile}
                          disabled={busy}
                          placeholder={t("remoteWizard.identityPlaceholder")}
                          onChange={(event) => set("identityFile", event.target.value)}
                        />
                        <button
                          type="button"
                          className="remote-wizard__pick-btn"
                          disabled={busy}
                          aria-label={t("remoteWizard.pickIdentityFile")}
                          title={t("remoteWizard.pickIdentityFile")}
                          onClick={() => {
                            void app.PickRemoteIdentityFile().then((path) => {
                              if (path) set("identityFile", path);
                            }).catch((e) => {
                              setError(e instanceof Error ? e.message : String(e));
                            });
                          }}
                        >
                          <Plus size={14} aria-hidden="true" />
                        </button>
                      </div>
                    </label>
                    <label className="remote-wizard__field">
                      <span>{t("remote.host.keyPassphrase")}</span>
                      <input
                        type="password"
                        autoComplete="new-password"
                        placeholder={t("remoteWizard.passphrasePlaceholder")}
                        value={form.keyPassphrase ?? ""}
                        disabled={busy}
                        onChange={(event) => setForm((current) => ({ ...current, keyPassphrase: event.target.value, clearPassphrase: false }))}
                      />
                    </label>
                  </>
                )}
                <div className="remote-wizard__field">
                  <span>{t("remoteWizard.downloadMethod")}</span>
                  <div className="provider-add-segmented remote-wizard__seg" role="group" aria-label={t("remoteWizard.downloadMethod")}>
                    <button
                      type="button"
                      className={`provider-add-segmented__item${form.serveInstall === "upload" ? " provider-add-segmented__item--active" : ""}`}
                      disabled={busy}
                      onClick={() => set("serveInstall", "upload")}
                    >
                      {t("remoteWizard.downloadUpload")}
                    </button>
                    <button
                      type="button"
                      className={`provider-add-segmented__item${form.serveInstall === "npm" ? " provider-add-segmented__item--active" : ""}`}
                      disabled={busy}
                      onClick={() => set("serveInstall", "npm")}
                    >
                      {t("remoteWizard.downloadRemote")}
                    </button>
                  </div>
                </div>
                <div className="remote-wizard__field">
                  <span>{t("remote.host.credentialMode")}</span>
                  <div className="provider-add-segmented remote-wizard__seg" role="group" aria-label={t("remote.host.credentialMode")}>
                    <button
                      type="button"
                      className={`provider-add-segmented__item${form.credentialMode !== "local-proxy" ? " provider-add-segmented__item--active" : ""}`}
                      disabled={busy}
                      onClick={() => set("credentialMode", "remote")}
                    >
                      {t("remote.host.credentialModeRemote")}
                    </button>
                    <button
                      type="button"
                      className={`provider-add-segmented__item${form.credentialMode === "local-proxy" ? " provider-add-segmented__item--active" : ""}`}
                      disabled={busy}
                      onClick={() => set("credentialMode", "local-proxy")}
                    >
                      {t("remote.host.credentialModeLocalProxy")}
                    </button>
                  </div>
                </div>
                </div>
              </>
            ) : null}

            {step === "connecting" ? (
              <div className="remote-wizard__connecting">
                <span className="remote-wizard__connecting-title">
                  {host ? `${host.label} · ${host.user ? `${host.user}@` : ""}${host.host}` : ""}
                </span>
                <RemoteStatusChip state={statuses[hostId]?.state ?? "connecting"} />
                <div className="remote-wizard__log" ref={logRef} role="log" aria-label={t("remoteWizard.stepConnecting")}>
                  {logLines.map((line, index) => (
                    <div key={index} className="remote-wizard__log-line">
                      <span className="remote-wizard__log-time">{line.time}</span>
                      <span className={`remote-wizard__log-level remote-wizard__log-level--${line.level}`}>{line.level.toUpperCase()}</span>
                      <span className="remote-wizard__log-text">{line.text}</span>
                    </div>
                  ))}
                </div>
                {connectErr ? (
                  <>
                    <div className="remote-wizard__error" role="alert">
                      {connectErr}
                    </div>
                    <div className="remote-wizard__connecting-actions">
                      <button type="button" className="btn btn--small" onClick={() => setStep("config")}>
                        {t("remoteWizard.backToEdit")}
                      </button>
                      <button type="button" className="btn btn--small btn--primary" onClick={() => void connect(hostId, startPath, host)}>
                        {t("remoteWizard.retry")}
                      </button>
                    </div>
                  </>
                ) : (
                  <span className="remote-wizard__connecting-hint">{t("remoteWizard.connecting")}</span>
                )}
              </div>
            ) : null}

            {step === "workspace" ? (
              <>
                <div className="remote-wizard__workspace-head">
                  <div className="remote-wizard__workspace-title">{t("remoteWizard.workspaceIntro")}</div>
                  <div className="remote-wizard__workspace-ready">
                    <span className="remote-wizard__ready-dot" aria-hidden="true" />
                    {t("remoteWizard.workspaceReady")}
                  </div>
                </div>
                <div className="remote-wizard__browse">{t("remoteWizard.browseFolder")}</div>
                <div className="remote-wizard__path-bar">
                  <input
                    className="remote-wizard__path-input"
                    value={workspace}
                    disabled={busy}
                    placeholder={t("remoteWizard.path")}
                    onChange={(event) => setWorkspace(event.target.value)}
                  />
                  <button type="button" className="btn btn--small" disabled={busy || !workspace.trim()} onClick={() => void openDir(hostId, workspace.trim() || "~")}>
                    {t("remoteWizard.go")}
                  </button>
                  <button type="button" className="btn btn--small" disabled={busy} onClick={() => setShowHidden((value) => !value)}>
                    {t(showHidden ? "remoteWizard.toggleHiddenOn" : "remoteWizard.toggleHidden")}
                  </button>
                </div>
                <div className="remote-wizard__tree">
                  {entries === null && !listErr ? <div className="remote-wizard__empty">{t("common.loading")}</div> : null}
                  {listErr ? (
                    <div className="remote-wizard__error" role="alert">
                      {listErr}
                    </div>
                  ) : null}
                  {entries?.length === 0 && !listErr ? <div className="remote-wizard__empty">{t("remoteWizard.emptyDir")}</div> : null}
                  {workspace && workspace !== "/" && workspace !== "~" ? (
                    <button
                      type="button"
                      className="remote-wizard__dir"
                      onClick={() => {
                        setSelectedFile(null);
                        void openDir(hostId, parentOf(workspace) === "/" ? "~" : parentOf(workspace));
                      }}
                    >
                      <Folder size={13} className="remote-wizard__dir-icon" aria-hidden="true" />
                      <span className="remote-wizard__dir-name">..</span>
                    </button>
                  ) : null}
                  {(entries ?? [])
                    .filter((entry) => showHidden || !entry.name.startsWith("."))
                    .map((entry) => entry.isDir ? { entry, rank: 0 } : { entry, rank: 1 })
                    .sort((a, b) => a.rank - b.rank || a.entry.name.localeCompare(b.entry.name))
                    .map(({ entry }) =>
                      entry.isDir ? (
                        <button
                          key={entry.path}
                          type="button"
                          className={`remote-wizard__dir${workspace === entry.path && !selectedFile ? " remote-wizard__dir--selected" : ""}`}
                          onClick={() => {
                            setSelectedFile(null);
                            void openDir(hostId, entry.path);
                          }}
                        >
                          <Folder size={13} className="remote-wizard__dir-icon" aria-hidden="true" />
                          <span className="remote-wizard__dir-name">{entry.name}</span>
                        </button>
                      ) : (
                        <button
                          key={entry.path}
                          type="button"
                          className={`remote-wizard__file${selectedFile === entry.path ? " remote-wizard__file--selected" : ""}`}
                          onClick={() => {
                            setSelectedFile(entry.path);
                            setWorkspace(parentOf(entry.path));
                          }}
                        >
                          <FileText size={13} className="remote-wizard__dir-icon" aria-hidden="true" />
                          <span className="remote-wizard__dir-name">{entry.name}</span>
                          <span className="remote-wizard__file-size">{formatBytes(entry.size)}</span>
                        </button>
                      ),
                    )}
                </div>

              </>
            ) : null}
          </div>
        </div>
        <div className="remote-wizard__footer">
          {error ? (
            <div className="remote-wizard__error" role="alert">
              <span className="remote-wizard__error-mark" aria-hidden="true">⚠</span>
              {error}
            </div>
          ) : (
            <div className="remote-wizard__error" />
          )}
          <div className="modal__actions remote-wizard__actions">
            {step !== "config" ? (
              <button type="button" className="btn btn--small" disabled={busy} onClick={() => setStep("config")}>
                {t("remoteWizard.back")}
              </button>
            ) : null}
            <button type="button" className="btn btn--small" disabled={busy} onClick={onClose}>
              {t("common.cancel")}
            </button>
            {step === "config" ? (
              <button type="button" className="btn btn--small btn--primary" disabled={busy} onClick={() => void nextFromConfig()}>
                {busy ? t("common.loading") : t("remoteWizard.next")}
              </button>
            ) : null}
            {step === "workspace" ? (
              <button type="button" className="btn btn--small btn--primary" disabled={busy || !workspace.trim()} onClick={() => void finish()}>
                {busy ? t("remoteWizard.connecting") : t("remoteWizard.finish")}
              </button>
            ) : null}
          </div>
        </div>
      </div>
    </div>,
    document.body,
  );
}
