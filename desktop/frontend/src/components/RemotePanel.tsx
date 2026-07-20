import { useCallback, useEffect, useRef, useState, type ReactNode } from "react";

import { app } from "../lib/bridge";
import { useT } from "../lib/i18n";
import { isRemoteDegradedWarning, isRemoteTerminalFailure, remoteConnectionErrorSummaryKey } from "../lib/remoteErrors";
import { useOverlayStore } from "../store/overlays";
import { useRemoteStore, type RemoteExplorerTab } from "../store/remote";
import type { RemoteDirEntry, RemoteForwardView } from "../lib/types";
import { CodeViewer } from "./CodeViewer";
import { RemoteStatusChip } from "./RemoteHostsPage";

const EMPTY_REMOTE_FORWARDS: RemoteForwardView[] = [];

/** RemotePanel is the right-dock remote work surface: a host header with
 *  Files / Ports / Server tabs. */
export function RemotePanel({ onClose }: { onClose: () => void }) {
  const t = useT();
  const hostId = useRemoteStore((s) => s.explorerHostId);
  const host = useRemoteStore((s) => s.hosts.find((item) => item.id === hostId));
  const tab = useRemoteStore((s) => s.explorerTab);
  const setTab = useRemoteStore((s) => s.setExplorerTab);
  const status = useRemoteStore((s) => (hostId ? s.statuses[hostId] : undefined));
  const setSettingsTarget = useOverlayStore((s) => s.setSettingsTarget);

  if (!hostId) return null;
  const connected = status?.state === "connected" || status?.state === "degraded";
  const busy = status?.state === "connecting" || status?.state === "reconnecting" || status?.state === "pending_hostkey" || status?.state === "pending_secret";
  const terminalFailure = isRemoteTerminalFailure(status);
  const degradedWarning = isRemoteDegradedWarning(status);
  const target = host ? `${host.user ? `${host.user}@` : ""}${host.host}${host.port && host.port !== 22 ? `:${host.port}` : ""}` : hostId;

  return (
    <section className="remote-panel" aria-label={t("remote.explorer")}>
      <header className="remote-panel__header">
        <span className="remote-panel__host-copy">
          <span className="remote-panel__host">{host?.label || hostId}</span>
          <span className="remote-panel__target">{target}</span>
        </span>
        <RemoteStatusChip state={status?.state ?? "stopped"} />
        <div className="remote-panel__header-actions">
          {connected ? (
            <button className="btn btn--small" onClick={() => void app.DisconnectRemoteHost(hostId).catch(() => {})}>
              {t("remote.disconnect")}
            </button>
          ) : (
            <button className="btn btn--small btn--primary" disabled={busy} onClick={() => void app.ConnectRemoteHost(hostId).catch(() => {})}>
              {busy ? t(`remote.status.${status?.state ?? "connecting"}`) : t("remote.connect")}
            </button>
          )}
          <button className="btn btn--ghost" onClick={() => setSettingsTarget("remote")}>
            {t("remote.manageHosts")}
          </button>
          <button className="btn btn--ghost" onClick={onClose} aria-label={t("rightDock.collapse")}>
            ×
          </button>
        </div>
      </header>

      {(terminalFailure || degradedWarning) && status && (
        <div className={`remote-panel__error-banner ${degradedWarning ? "remote-panel__error-banner--warning" : ""}`} role="alert">
          <strong>{t(degradedWarning ? "remote.status.degraded" : "remote.status.failed")}</strong>
          <span>{t(remoteConnectionErrorSummaryKey(status), { host: host?.label || hostId })}</span>
        </div>
      )}

      {status?.state === "reconnecting" && (
        <div className="remote-panel__banner" role="status">
          {t("remote.banner.reconnecting", { n: status.attempt ?? 1 })}
        </div>
      )}

      <nav className="remote-panel__tabs" role="tablist">
        {(["files", "ports", "server"] as RemoteExplorerTab[]).map((id) => (
          <button
            key={id}
            role="tab"
            aria-selected={tab === id}
            className={`remote-panel__tab ${tab === id ? "is-active" : ""}`}
            onClick={() => setTab(id)}
          >
            {t(`remote.tab.${id}`)}
          </button>
        ))}
      </nav>

      <div className="remote-panel__body">
        {tab === "files" && <RemoteFilesTab hostId={hostId} connected={connected} />}
        {tab === "ports" && <RemotePortsTab hostId={hostId} connected={connected} />}
        {tab === "server" && <RemoteServerTab hostId={hostId} connected={connected} />}
      </div>
    </section>
  );
}

// ── Files tab: lean lazy tree + preview/edit ──

function RemoteFilesTab({ hostId, connected }: { hostId: string; connected: boolean }) {
  const t = useT();
  const [entriesByDir, setEntriesByDir] = useState<Record<string, RemoteDirEntry[]>>({});
  const [openDirs, setOpenDirs] = useState<Set<string>>(new Set());
  const [selected, setSelected] = useState<string | null>(null);
  const [loadErr, setLoadErr] = useState("");
  const rootPath = "."; // remote home; RealPath resolves it server-side

  const loadDir = useCallback(
    async (path: string) => {
      try {
        const entries = await app.ListRemoteDir(hostId, path);
        setEntriesByDir((m) => ({ ...m, [path]: entries }));
        setLoadErr("");
      } catch (e) {
        setLoadErr(t("remote.tree.loadError", { err: String(e) }));
      }
    },
    [hostId, t],
  );

  useEffect(() => {
    if (connected) void loadDir(rootPath);
  }, [connected, loadDir]);

  const toggleDir = (path: string) => {
    setOpenDirs((prev) => {
      const next = new Set(prev);
      if (next.has(path)) {
        next.delete(path);
      } else {
        next.add(path);
        if (!entriesByDir[path]) void loadDir(path);
      }
      return next;
    });
  };

  const renderDir = (path: string, depth: number): ReactNode => {
    const entries = entriesByDir[path];
    if (!entries) return null;
    if (entries.length === 0) return <li className="remote-tree__empty">{t("remote.tree.empty")}</li>;
    return entries.map((e) => (
      <li key={e.path} className="remote-tree__item" style={{ paddingLeft: depth * 12 }}>
        {e.isDir ? (
          <>
            <button className="remote-tree__row" onClick={() => toggleDir(e.path)} role="treeitem" aria-expanded={openDirs.has(e.path)}>
              {openDirs.has(e.path) ? "▾" : "▸"} {e.name}/
            </button>
            {openDirs.has(e.path) && <ul>{renderDir(e.path, depth + 1)}</ul>}
          </>
        ) : (
          <button
            className={`remote-tree__row ${selected === e.path ? "is-selected" : ""}`}
            onClick={() => setSelected(e.path)}
            role="treeitem"
          >
            {e.name}
          </button>
        )}
      </li>
    ));
  };

  if (!connected) return <p className="remote-panel__hint">{t("remote.status.stopped")}</p>;

  return (
    <div className="remote-files">
      <div className="remote-files__tree" role="tree">
        {loadErr && <p className="remote-panel__error" role="alert">{loadErr}</p>}
        <ul>{renderDir(rootPath, 0)}</ul>
      </div>
      <div className="remote-files__view">
        {selected ? <RemoteFileView hostId={hostId} path={selected} connected={connected} /> : null}
      </div>
    </div>
  );
}

function RemoteFileView({ hostId, path, connected }: { hostId: string; path: string; connected: boolean }) {
  const t = useT();
  const [body, setBody] = useState("");
  const [draft, setDraft] = useState<string | null>(null);
  const [mtime, setMtime] = useState(0);
  const [binary, setBinary] = useState(false);
  const [truncated, setTruncated] = useState(false);
  const [saving, setSaving] = useState(false);
  const [conflict, setConflict] = useState(false);
  const [err, setErr] = useState("");

  const load = useCallback(async () => {
    const p = await app.ReadRemoteFile(hostId, path);
    setBody(p.body);
    setDraft(null);
    setMtime(p.mtimeUnix);
    setBinary(p.binary);
    setTruncated(p.truncated);
    setErr(p.err ?? "");
  }, [hostId, path]);

  useEffect(() => {
    void load();
  }, [load]);

  const editable = connected && !binary && !truncated && !err;
  const dirty = draft !== null && draft !== body;

  const save = async (force: boolean) => {
    if (draft === null) return;
    setSaving(true);
    try {
      const res = await app.WriteRemoteFile(hostId, path, draft, force ? 0 : mtime);
      if (res.conflict) {
        setConflict(true);
        return;
      }
      setBody(draft);
      setDraft(null);
      setMtime(res.newMtimeUnix);
      setConflict(false);
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="remote-file-view">
      <div className="remote-file-view__toolbar">
        <span className="remote-file-view__path">{path}</span>
        {err && <span className="remote-panel__error">{err}</span>}
        {binary && <span className="remote-panel__hint">{t("remote.editor.binaryBlocked")}</span>}
        {truncated && <span className="remote-panel__hint">{t("remote.editor.truncatedBlocked")}</span>}
        {editable && draft === null && (
          <button className="btn" onClick={() => setDraft(body)}>{t("remote.editor.edit")}</button>
        )}
        {draft !== null && (
          <button className="btn btn--primary" disabled={saving || !dirty || !connected} onClick={() => void save(false)}>
            {saving ? t("remote.editor.saving") : t("remote.editor.save")}
          </button>
        )}
        {draft !== null && !connected && <span className="remote-panel__hint">{t("remote.editor.readOnlyDisconnected")}</span>}
      </div>
      {draft === null ? (
        <CodeViewer value={body} readOnly />
      ) : (
        <textarea
          className="remote-file-view__editor"
          value={draft}
          spellCheck={false}
          onChange={(e) => setDraft(e.target.value)}
        />
      )}
      {conflict && (
        <div className="remote-file-view__conflict" role="alertdialog">
          <p><strong>{t("remote.editor.conflictTitle")}</strong></p>
          <p>{t("remote.editor.conflictBody")}</p>
          <button className="btn" onClick={() => void load()}>{t("remote.editor.reload")}</button>
          <button className="btn btn--danger" onClick={() => void save(true)}>{t("remote.editor.overwrite")}</button>
        </div>
      )}
    </div>
  );
}

// ── Ports tab ──

function RemotePortsTab({ hostId, connected }: { hostId: string; connected: boolean }) {
  const t = useT();
  const forwards = useRemoteStore((s) => s.forwards[hostId] ?? EMPTY_REMOTE_FORWARDS);
  const setForwards = useRemoteStore((s) => s.setForwards);
  const [localPort, setLocalPort] = useState(8080);
  const [remoteHost, setRemoteHost] = useState("127.0.0.1");
  const [remotePort, setRemotePort] = useState(80);
  const [label, setLabel] = useState("");
  const [actionErr, setActionErr] = useState("");

  useEffect(() => {
    if (connected) void app.RemoteForwards(hostId).then((f) => setForwards(hostId, f));
  }, [hostId, connected, setForwards]);

  const add = async () => {
    try {
      await app.AddRemoteForward(hostId, { localPort, remoteHost, remotePort, label });
      setLabel("");
      setActionErr("");
    } catch (e) {
      setActionErr(String(e));
    }
  };

  const remove = async (forwardId: string) => {
    try {
      await app.RemoveRemoteForward(hostId, forwardId);
      setActionErr("");
    } catch (e) {
      setActionErr(String(e));
    }
  };

  return (
    <div className="remote-ports">
      {actionErr && <p className="remote-panel__error" role="alert">{actionErr}</p>}
      {forwards.length === 0 ? (
        <p className="remote-panel__hint">{t("remote.ports.empty")}</p>
      ) : (
        <ul className="remote-ports__list">
          {forwards.map((f: RemoteForwardView) => (
            <li key={f.id} className="remote-ports__row">
              <span className={`remote-dot remote-dot--${f.state}`} aria-hidden />
              <span>{f.label || f.id}</span>
              {f.error && <span className="remote-panel__error">{f.error}</span>}
              <button className="btn btn--ghost" onClick={() => void remove(f.id)}>
                {t("remote.ports.remove")}
              </button>
            </li>
          ))}
        </ul>
      )}
      <div className="remote-ports__form">
        <input type="number" min={1} max={65535} aria-label={t("remote.ports.localPort")} value={localPort} onChange={(e) => setLocalPort(Number(e.target.value) || 0)} />
        <input aria-label={t("remote.ports.remoteHost")} value={remoteHost} onChange={(e) => setRemoteHost(e.target.value)} />
        <input type="number" min={1} max={65535} aria-label={t("remote.ports.remotePort")} value={remotePort} onChange={(e) => setRemotePort(Number(e.target.value) || 0)} />
        <input aria-label={t("remote.ports.label")} placeholder={t("remote.ports.label")} value={label} onChange={(e) => setLabel(e.target.value)} />
        <button className="btn btn--primary" disabled={!connected || !remoteHost.trim() || localPort < 1 || localPort > 65535 || remotePort < 1 || remotePort > 65535} onClick={() => void add()}>{t("remote.ports.add")}</button>
      </div>
    </div>
  );
}

// ── Server tab ──

function RemoteServerTab({ hostId, connected }: { hostId: string; connected: boolean }) {
  const t = useT();
  const server = useRemoteStore((s) => s.servers[hostId]);
  const setServer = useRemoteStore((s) => s.setServer);
  const [workspace, setWorkspace] = useState("");
  const [logs, setLogs] = useState("");
  const [actionErr, setActionErr] = useState("");
  const logsOpen = useRef(false);

  useEffect(() => {
    void app.RemoteLastWorkspace(hostId).then((w) => setWorkspace((cur) => cur || w));
    void app.RemoteServerStatus(hostId).then(setServer);
  }, [hostId, setServer]);

  const refreshLogs = async () => {
    logsOpen.current = true;
    try {
      setLogs(await app.RemoteServerLogs(hostId, 200));
      setActionErr("");
    } catch (e) {
      setLogs("");
      setActionErr(String(e));
    }
  };

  const start = async () => {
    try {
      setActionErr("");
      await app.OpenRemoteWorkspace(hostId, workspace);
    } catch (e) {
      setActionErr(String(e));
    }
  };

  const stop = async () => {
    try {
      setActionErr("");
      await app.StopRemoteServer(hostId);
    } catch (e) {
      setActionErr(String(e));
    }
  };

  const state = server?.state ?? "stopped";
  const busy = ["starting", "detect", "install", "waiting_lock", "launch", "health_check", "reuse"].includes(state);
  const stateLabel = state === "ready"
    ? t("remote.server.state.ready")
    : state === "error"
      ? t("remote.server.state.error")
      : busy
        ? t("remote.server.state.starting")
        : t("remote.server.state.stopped");
  const canManageServer = connected && Boolean(server?.workspace) && state !== "stopped";
  return (
    <div className="remote-server">
      <label className="remote-server__ws">
        {t("remote.server.workspace")}
        <input value={workspace} onChange={(e) => setWorkspace(e.target.value)} placeholder="~/project" />
      </label>
      <div className="remote-server__status">
        {stateLabel}
        {server?.message ? ` — ${server.message}` : ""}
        {server?.error ? ` — ${server.error}` : ""}
        {actionErr ? ` — ${actionErr}` : ""}
      </div>
      <div className="remote-server__actions">
        <button className="btn btn--primary" disabled={!connected || !workspace || busy} onClick={() => void start()}>
          {t("remote.server.start")}
        </button>
        {server?.localUrl && (
          <button className="btn" onClick={() => void app.OpenRemoteWorkspace(hostId, workspace)}>
            {t("remote.server.openBrowser")}
          </button>
        )}
        <button className="btn" disabled={!canManageServer || busy} onClick={() => void stop()}>
          {t("remote.server.stop")}
        </button>
        <button className="btn btn--ghost" disabled={!canManageServer} onClick={() => void refreshLogs()}>
          {t("remote.server.logs")}
        </button>
      </div>
      {logsOpen.current && (
        <pre className="remote-server__logs">
          {logs}
          <button className="btn btn--ghost" onClick={() => void refreshLogs()}>{t("remote.server.refreshLogs")}</button>
        </pre>
      )}
    </div>
  );
}
