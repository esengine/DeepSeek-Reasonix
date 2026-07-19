import { useCallback, useEffect, useState } from "react";

import { app } from "../lib/bridge";
import { useT } from "../lib/i18n";
import { useRemoteStore } from "../store/remote";
import type { RemoteConnectionStatus, RemoteHostInput, RemoteHostView, RemoteConnState } from "../lib/types";

const EMPTY_INPUT: RemoteHostInput = {
  label: "",
  host: "",
  port: 22,
  user: "",
  identityFile: "",
  proxyJump: "",
  defaultWorkspace: "",
  serveInstall: "auto",
  useSSHConfig: false,
};

type Screen = { kind: "list" } | { kind: "add" } | { kind: "edit"; id: string } | { kind: "import" };

/** RemoteHostsPage is the Settings > Remote SSH manager: host list with
 *  connect/disconnect + add/edit/remove + ssh_config import. */
export function RemoteHostsPage() {
  const t = useT();
  const [hosts, setHosts] = useState<RemoteHostView[]>([]);
  const [screen, setScreen] = useState<Screen>({ kind: "list" });
  const statuses = useRemoteStore((s) => s.statuses);
  const setStoreHosts = useRemoteStore((s) => s.setHosts);
  const hydrateStatuses = useRemoteStore((s) => s.hydrateStatuses);
  const openExplorer = useRemoteStore((s) => s.openExplorer);

  const refresh = useCallback(async () => {
    const next = await app.RemoteHosts();
    setHosts(next);
    setStoreHosts(next);
  }, [setStoreHosts]);

  useEffect(() => {
    void refresh();
    void app.RemoteConnectionStatuses().then(hydrateStatuses);
  }, [refresh, hydrateStatuses]);

  if (screen.kind === "add" || screen.kind === "edit") {
    const initial =
      screen.kind === "edit" ? hostToInput(hosts.find((h) => h.id === screen.id)) : EMPTY_INPUT;
    return (
      <RemoteHostForm
        initial={initial}
        editingId={screen.kind === "edit" ? screen.id : null}
        onDone={async () => {
          await refresh();
          setScreen({ kind: "list" });
        }}
        onCancel={() => setScreen({ kind: "list" })}
      />
    );
  }
  if (screen.kind === "import") {
    return (
      <RemoteSSHConfigImport
        onDone={async () => {
          await refresh();
          setScreen({ kind: "list" });
        }}
        onCancel={() => setScreen({ kind: "list" })}
      />
    );
  }

  return (
    <div className="remote-hosts">
      <div className="remote-hosts__toolbar">
        <h2>{t("remote.hosts.title")}</h2>
        <div className="remote-hosts__actions">
          <button className="btn" onClick={() => setScreen({ kind: "import" })}>
            {t("remote.hosts.import")}
          </button>
          <button className="btn btn--primary" onClick={() => setScreen({ kind: "add" })}>
            {t("remote.hosts.add")}
          </button>
        </div>
      </div>
      {hosts.length === 0 ? (
        <p className="remote-hosts__empty">{t("remote.hosts.empty")}</p>
      ) : (
        <ul className="remote-hosts__list">
          {hosts.map((h) => (
            <RemoteHostRow
              key={h.id}
              host={h}
              status={statuses[h.id]}
              onConnect={() => void app.ConnectRemoteHost(h.id).catch(() => {})}
              onDisconnect={() => void app.DisconnectRemoteHost(h.id).catch(() => {})}
              onOpen={() => openExplorer(h.id)}
              onEdit={() => setScreen({ kind: "edit", id: h.id })}
              onRemove={async () => {
                await app.RemoveRemoteHost(h.id);
                await refresh();
              }}
            />
          ))}
        </ul>
      )}
    </div>
  );
}

function RemoteHostRow(props: {
  host: RemoteHostView;
  status?: RemoteConnectionStatus;
  onConnect: () => void;
  onDisconnect: () => void;
  onOpen: () => void;
  onEdit: () => void;
  onRemove: () => void;
}) {
  const t = useT();
  const { host } = props;
  const state = props.status?.state;
  const target = `${host.user ? host.user + "@" : ""}${host.host}${host.port && host.port !== 22 ? ":" + host.port : ""}`;
  const connected = state === "connected" || state === "degraded";
  return (
    <li className="remote-host-row">
      <div className="remote-host-row__main">
        <span className="remote-host-row__name">{host.label}</span>
        <span className="remote-host-row__target">{target}</span>
        {state && <RemoteStatusChip state={state} />}
        {props.status?.error && <span className="remote-panel__error">{props.status.error}</span>}
      </div>
      <div className="remote-host-row__actions">
        {connected ? (
          <>
            <button className="btn" onClick={props.onOpen}>
              {t("remote.explorer")}
            </button>
            <button className="btn" onClick={props.onDisconnect}>
              {t("remote.disconnect")}
            </button>
          </>
        ) : (
          <button className="btn btn--primary" onClick={props.onConnect}>
            {t("remote.connect")}
          </button>
        )}
        <button className="btn" onClick={props.onEdit}>
          {t("remote.host.edit")}
        </button>
        <button
          className="btn btn--danger"
          onClick={() => {
            if (confirm(t("remote.host.removeConfirm"))) props.onRemove();
          }}
        >
          {t("remote.host.remove")}
        </button>
      </div>
    </li>
  );
}

export function RemoteStatusChip({ state }: { state: RemoteConnState }) {
  const t = useT();
  return (
    <span className={`remote-chip remote-chip--${state}`} aria-label={t(`remote.status.${state}`)}>
      {t(`remote.status.${state}`)}
    </span>
  );
}

function RemoteHostForm(props: {
  initial: RemoteHostInput;
  editingId: string | null;
  onDone: () => void;
  onCancel: () => void;
}) {
  const t = useT();
  const [form, setForm] = useState<RemoteHostInput>(props.initial);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");

  const set = <K extends keyof RemoteHostInput>(k: K, v: RemoteHostInput[K]) =>
    setForm((f) => ({ ...f, [k]: v }));

  const submit = async () => {
    setBusy(true);
    setErr("");
    try {
      if (props.editingId) await app.UpdateRemoteHost(props.editingId, form);
      else await app.AddRemoteHost(form);
      props.onDone();
    } catch (e) {
      setErr(String(e));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="remote-host-form">
      <label>
        {t("remote.host.label")}
        <input value={form.label} disabled={!!props.editingId} onChange={(e) => set("label", e.target.value)} />
      </label>
      <label>
        {t("remote.host.host")}
        <input value={form.host} onChange={(e) => set("host", e.target.value)} />
      </label>
      <label>
        {t("remote.host.port")}
        <input type="number" min={1} max={65535} value={form.port} onChange={(e) => set("port", Number(e.target.value) || 0)} />
      </label>
      <label>
        <input type="checkbox" checked={form.useSSHConfig} onChange={(e) => set("useSSHConfig", e.target.checked)} />
        {t("remote.host.useSSHConfig")}
      </label>
      <label>
        {t("remote.host.user")}
        <input value={form.user} onChange={(e) => set("user", e.target.value)} />
      </label>
      <label>
        {t("remote.host.identityFile")}
        <input value={form.identityFile} onChange={(e) => set("identityFile", e.target.value)} />
      </label>
      <label>
        {t("remote.host.proxyJump")}
        <input value={form.proxyJump} onChange={(e) => set("proxyJump", e.target.value)} />
      </label>
      <label>
        {t("remote.host.defaultWorkspace")}
        <input value={form.defaultWorkspace} onChange={(e) => set("defaultWorkspace", e.target.value)} />
      </label>
      <label>
        {t("remote.host.serveInstall")}
        <select value={form.serveInstall} onChange={(e) => set("serveInstall", e.target.value)}>
          <option value="auto">auto</option>
          <option value="npm">npm</option>
          <option value="upload">upload</option>
          <option value="never">never</option>
        </select>
      </label>
      {err && <p className="remote-host-form__error" role="alert">{err}</p>}
      <div className="remote-host-form__actions">
        <button className="btn" onClick={props.onCancel}>{t("remote.host.cancel")}</button>
        <button className="btn btn--primary" disabled={busy || !form.label.trim() || !form.host.trim() || form.port < 1 || form.port > 65535} onClick={() => void submit()}>
          {t("remote.host.save")}
        </button>
      </div>
    </div>
  );
}

function RemoteSSHConfigImport(props: { onDone: () => void; onCancel: () => void }) {
  const t = useT();
  const [candidates, setCandidates] = useState<RemoteHostInput[]>([]);
  const [selected, setSelected] = useState<Record<string, boolean>>({});
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");

  useEffect(() => {
    void app.ScanSSHConfig().then(setCandidates).catch((e) => setErr(String(e)));
  }, []);

  const importSelected = async () => {
    setBusy(true);
    setErr("");
    try {
      for (const c of candidates) {
        if (selected[c.label]) await app.AddRemoteHost(c);
      }
      props.onDone();
    } catch (e) {
      setErr(String(e));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="remote-import">
      {err && <p className="remote-host-form__error" role="alert">{err}</p>}
      {candidates.length === 0 ? (
        <p className="remote-hosts__empty">{t("remote.hosts.importEmpty")}</p>
      ) : (
        <ul className="remote-import__list">
          {candidates.map((c) => (
            <li key={c.label}>
              <label>
                <input
                  type="checkbox"
                  checked={!!selected[c.label]}
                  onChange={(e) => setSelected((s) => ({ ...s, [c.label]: e.target.checked }))}
                />
                {c.label} — {c.user ? c.user + "@" : ""}{c.host}
              </label>
            </li>
          ))}
        </ul>
      )}
      <div className="remote-host-form__actions">
        <button className="btn" onClick={props.onCancel}>{t("remote.host.cancel")}</button>
        <button className="btn btn--primary" disabled={busy || !Object.values(selected).some(Boolean)} onClick={() => void importSelected()}>
          {t("remote.hosts.importSelected")}
        </button>
      </div>
    </div>
  );
}

function hostToInput(h?: RemoteHostView): RemoteHostInput {
  if (!h) return EMPTY_INPUT;
  return {
    label: h.label,
    host: h.host,
    port: h.port,
    user: h.user,
    identityFile: h.identityFile,
    proxyJump: h.proxyJump,
    defaultWorkspace: h.defaultWorkspace,
    serveInstall: h.serveInstall,
    useSSHConfig: h.useSSHConfig,
  };
}
