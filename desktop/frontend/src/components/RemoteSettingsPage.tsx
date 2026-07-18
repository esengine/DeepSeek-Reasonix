import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Loader2, Pencil, Plus, RefreshCw, Server, Trash2 } from "lucide-react";

import { app, onRemoteTargetState } from "../lib/bridge";
import { useT } from "../lib/i18n";
import type { RemoteConnectionLogView, RemoteHostInput, RemoteHostRuntimeSummaryView, RemoteHostView, RemoteTargetState, RemoteTargetStatusView } from "../lib/types";

const LOCAL_STATUS: RemoteTargetStatusView = { state: "LocalConnected", canReconnect: false };

function errorText(error: unknown): string {
  if (error && typeof error === "object" && "message" in error && typeof error.message === "string") {
    return error.message;
  }
  return String(error);
}

function targetStateKey(state: RemoteTargetState) {
  return `remote.state.${state}` as const;
}

function isTargetTransition(state: RemoteTargetState): boolean {
  return state === "RemoteConnecting" || state === "RemoteReconnecting" || state === "Switching";
}

function isRemoteTarget(state: RemoteTargetState): boolean {
  return state === "RemoteConnected" || state === "RemoteReconnecting" || state === "RemoteConnecting";
}

function formatLogTime(atMillis: number): string {
  if (!Number.isFinite(atMillis) || atMillis <= 0) return "—";
  try {
    return new Intl.DateTimeFormat(undefined, {
      hour: "2-digit",
      minute: "2-digit",
      second: "2-digit",
    }).format(new Date(atMillis));
  } catch {
    return "—";
  }
}

function logDateTime(atMillis: number): string | undefined {
  if (!Number.isFinite(atMillis) || atMillis <= 0) return undefined;
  const value = new Date(atMillis);
  return Number.isNaN(value.getTime()) ? undefined : value.toISOString();
}

export function RemoteSettingsPage() {
  const t = useT();
  const [hosts, setHosts] = useState<RemoteHostView[]>([]);
  const [status, setStatus] = useState<RemoteTargetStatusView>(LOCAL_STATUS);
  const [selectedId, setSelectedId] = useState("");
  const [editingId, setEditingId] = useState<string | undefined>();
  const [alias, setAlias] = useState("");
  const [label, setLabel] = useState("");
  const [sshConfigPath, setSSHConfigPath] = useState("");
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [confirmDelete, setConfirmDelete] = useState(false);
  const [confirmLocal, setConfirmLocal] = useState(false);
  const [logs, setLogs] = useState<RemoteConnectionLogView[]>([]);
  const [logsLoading, setLogsLoading] = useState(false);
  const [logsError, setLogsError] = useState("");
  const logsRequest = useRef(0);
  const [runtimeSummary, setRuntimeSummary] = useState<RemoteHostRuntimeSummaryView | null>(null);
  const [runtimeSummaryLoading, setRuntimeSummaryLoading] = useState(false);
  const [runtimeSummaryError, setRuntimeSummaryError] = useState("");

  const loadLogs = useCallback(async () => {
    const request = ++logsRequest.current;
    setLogsLoading(true);
    setLogsError("");
    try {
      const next = await app.RemoteConnectionLogs();
      if (request === logsRequest.current) setLogs(Array.isArray(next) ? next : []);
    } catch (cause) {
      if (request === logsRequest.current) setLogsError(errorText(cause));
    } finally {
      if (request === logsRequest.current) setLogsLoading(false);
    }
  }, []);

  const reload = useCallback(async () => {
    setLoading(true);
    setError("");
    try {
      const [nextHosts, nextStatus] = await Promise.all([app.RemoteHosts(), app.RemoteTargetStatus()]);
      setHosts(Array.isArray(nextHosts) ? nextHosts : []);
      setStatus(nextStatus);
      setSelectedId((current) => current && nextHosts.some((host) => host.id === current)
        ? current
        : nextStatus.hostId && nextHosts.some((host) => host.id === nextStatus.hostId)
          ? nextStatus.hostId
          : nextHosts[0]?.id ?? "");
    } catch (cause) {
      setError(errorText(cause));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void reload();
    void loadLogs();
    const offTarget = onRemoteTargetState((next) => {
      setStatus(next);
      setConfirmLocal(false);
      void loadLogs();
    });
    return () => {
      logsRequest.current += 1;
      offTarget();
    };
  }, [loadLogs, reload]);

  const loadRuntimeSummary = useCallback(async () => {
    if (status.state !== "RemoteConnected") {
      setRuntimeSummary(null);
      setRuntimeSummaryError("");
      setRuntimeSummaryLoading(false);
      return;
    }
    setRuntimeSummaryLoading(true);
    setRuntimeSummaryError("");
    try {
      setRuntimeSummary(await app.RemoteHostRuntimeSummary());
    } catch (cause) {
      setRuntimeSummary(null);
      setRuntimeSummaryError(errorText(cause));
    } finally {
      setRuntimeSummaryLoading(false);
    }
  }, [status.hostId, status.state]);

  useEffect(() => {
    void loadRuntimeSummary();
  }, [loadRuntimeSummary]);

  const selected = useMemo(() => hosts.find((host) => host.id === selectedId), [hosts, selectedId]);
  const selectedIsCurrent = Boolean(selected && status.hostId === selected.id && isRemoteTarget(status.state));
  const selectedHasRecovery = Boolean(selected && status.hostId === selected.id && status.canReconnect);
  const transition = isTargetTransition(status.state);

  const startAdd = useCallback(() => {
    setEditingId(undefined);
    setAlias("");
    setLabel("");
    setSSHConfigPath("");
    setConfirmDelete(false);
    setError("");
  }, []);

  const startEdit = useCallback((host: RemoteHostView) => {
    setEditingId(host.id);
    setAlias(host.alias);
    setLabel(host.label);
    setSSHConfigPath(host.sshConfigPath ?? "");
    setConfirmDelete(false);
    setError("");
  }, []);

  const apply = useCallback(async (operation: () => Promise<void>) => {
    setBusy(true);
    setError("");
    try {
      await operation();
    } catch (cause) {
      setError(errorText(cause));
    } finally {
      setBusy(false);
    }
  }, []);

  const saveHost = useCallback(() => apply(async () => {
    const input: RemoteHostInput = {
      ...(editingId ? { id: editingId } : {}),
      alias: alias.trim(),
      label: label.trim(),
      ...(sshConfigPath.trim() ? { sshConfigPath: sshConfigPath.trim() } : {}),
    };
    const saved = await app.SaveRemoteHost(input);
    setHosts((current) => editingId
      ? current.map((host) => host.id === saved.id ? saved : host)
      : [...current, saved]);
    setSelectedId(saved.id);
    setEditingId(saved.id);
    setAlias(saved.alias);
    setLabel(saved.label);
    setSSHConfigPath(saved.sshConfigPath ?? "");
  }), [alias, apply, editingId, label, sshConfigPath]);

  const connect = useCallback(() => {
    if (!selected) return;
    void apply(async () => {
      await app.ConnectRemoteHost(selected.id);
      setStatus(await app.RemoteTargetStatus());
      await loadLogs();
    });
  }, [apply, loadLogs, selected]);

  const reconnect = useCallback(() => {
    void apply(async () => {
      await app.ReconnectRemoteTarget();
      setStatus(await app.RemoteTargetStatus());
      await loadLogs();
    });
  }, [apply, loadLogs]);

  const switchLocal = useCallback(() => {
    void apply(async () => {
      await app.SwitchToLocalTarget(true);
      setStatus(await app.RemoteTargetStatus());
      await loadLogs();
      setConfirmLocal(false);
    });
  }, [apply, loadLogs]);

  const deleteHost = useCallback(() => {
    if (!selected) return;
    void apply(async () => {
      await app.DeleteRemoteHost(selected.id);
      const next = hosts.filter((host) => host.id !== selected.id);
      setHosts(next);
      setSelectedId(next[0]?.id ?? "");
      if (editingId === selected.id) startAdd();
      setConfirmDelete(false);
    });
  }, [apply, editingId, hosts, selected, startAdd]);

  return (
    <div className="remote-settings" data-testid="remote-settings-page">
      <section className="settings-section remote-target-status" aria-live="polite">
        <div className="settings-section__head">
          <div>
            <div className="settings-section__title">{t("remote.status.title")}</div>
            <div className="settings-section__desc">{t("remote.status.description")}</div>
          </div>
          <span className={`remote-state remote-state--${status.state.toLowerCase()}`}>
            {t(targetStateKey(status.state))}
          </span>
        </div>
        {status.hostLabel && (
          <div className="remote-target-status__host">
            <Server size={16} aria-hidden="true" />
            <span>{status.hostLabel}</span>
          </div>
        )}
        {status.failure && <div className="banner banner--error" role="alert">{status.failure}</div>}
        <div className="remote-target-status__actions">
          {status.canReconnect && (
            <button className="btn btn--small" type="button" disabled={busy || transition} onClick={reconnect}>
              <RefreshCw size={14} aria-hidden="true" /> {t("remote.action.reconnect")}
            </button>
          )}
          {isRemoteTarget(status.state) && !confirmLocal && (
            <button className="btn btn--small" type="button" disabled={busy || transition} onClick={() => setConfirmLocal(true)}>
              {t("remote.action.switchLocal")}
            </button>
          )}
          {confirmLocal && (
            <div className="remote-inline-confirm" role="group" aria-label={t("remote.switch.confirmTitle")}>
              <span>{t("remote.switch.confirm")}</span>
              <button className="btn btn--small btn--danger" type="button" disabled={busy} onClick={switchLocal}>{t("common.confirm")}</button>
              <button className="btn btn--small" type="button" disabled={busy} onClick={() => setConfirmLocal(false)}>{t("common.cancel")}</button>
            </div>
          )}
        </div>
      </section>

      {status.state === "RemoteConnected" && (
        <RemoteHostRuntimeSection
          summary={runtimeSummary}
          loading={runtimeSummaryLoading}
          error={runtimeSummaryError}
          onRefresh={loadRuntimeSummary}
        />
      )}

      <section className="settings-section remote-connection-logs" aria-label={t("remote.logs.title")}>
        <div className="settings-section__head">
          <div>
            <div className="settings-section__title">{t("remote.logs.title")}</div>
            <div className="settings-section__desc">{t("remote.logs.description")}</div>
          </div>
          <button className="btn btn--small" type="button" disabled={logsLoading} onClick={() => void loadLogs()}>
            {logsLoading ? <Loader2 className="spin" size={14} aria-hidden="true" /> : <RefreshCw size={14} aria-hidden="true" />}
            {t("remote.logs.refresh")}
          </button>
        </div>
        {logsError && <div className="banner banner--error" role="alert">{logsError}</div>}
        {!logsLoading && !logsError && logs.length === 0 ? (
          <div className="empty remote-connection-logs__empty">{t("remote.logs.empty")}</div>
        ) : (
          <ol className="remote-connection-log-list" aria-live="polite">
            {[...logs].reverse().map((entry, index) => (
              <li key={`${entry.atMillis}-${entry.state}-${index}`}>
                <time dateTime={logDateTime(entry.atMillis)}>{formatLogTime(entry.atMillis)}</time>
                <span className={`remote-state remote-state--${entry.state.toLowerCase()}`}>{t(targetStateKey(entry.state))}</span>
                <span className="remote-connection-log-list__detail">
                  {entry.hostLabel && <strong>{entry.hostLabel}</strong>}
                  {entry.message && <span>{entry.message}</span>}
                </span>
              </li>
            ))}
          </ol>
        )}
      </section>

      {error && <div className="banner banner--error" role="alert">{error}</div>}

      <section className="settings-section remote-host-manager">
        <div className="settings-section__head">
          <div>
            <div className="settings-section__title">{t("remote.hosts.title")}</div>
            <div className="settings-section__desc">{t("remote.hosts.description")}</div>
          </div>
          <button className="btn btn--small" type="button" disabled={busy} onClick={startAdd}>
            <Plus size={14} aria-hidden="true" /> {t("remote.host.add")}
          </button>
        </div>

        {loading ? (
          <div className="empty"><Loader2 className="spin" size={16} aria-hidden="true" /> {t("settings.loading")}</div>
        ) : hosts.length === 0 ? (
          <div className="empty">{t("remote.hosts.empty")}</div>
        ) : (
          <div className="remote-host-list" role="listbox" aria-label={t("remote.hosts.title")}>
            {hosts.map((host) => (
              <button
                className={`remote-host-card${selectedId === host.id ? " remote-host-card--selected" : ""}`}
                type="button"
                role="option"
                aria-selected={selectedId === host.id}
                key={host.id}
                onClick={() => {
                  setSelectedId(host.id);
                  setConfirmDelete(false);
                }}
              >
                <span className="remote-host-card__copy">
                  <strong>{host.label}</strong>
                  <code>{host.alias}</code>
                  {host.sshConfigPath && <small>{host.sshConfigPath}</small>}
                </span>
                {status.hostId === host.id && <span className="remote-host-card__active">{t("remote.host.current")}</span>}
              </button>
            ))}
          </div>
        )}

        {selected && (
          <div className="remote-host-actions">
            <button className="btn btn--small btn--primary" type="button" disabled={busy || transition || selectedIsCurrent} onClick={connect}>
              {busy && <Loader2 className="spin" size={14} aria-hidden="true" />} {t("remote.action.connect")}
            </button>
            <button className="btn btn--small" type="button" disabled={busy} onClick={() => startEdit(selected)}>
              <Pencil size={14} aria-hidden="true" /> {t("common.edit")}
            </button>
            {!confirmDelete ? (
              <button className="btn btn--small" type="button" disabled={busy || selectedIsCurrent || selectedHasRecovery} onClick={() => setConfirmDelete(true)}>
                <Trash2 size={14} aria-hidden="true" /> {t("common.delete")}
              </button>
            ) : (
              <div className="remote-inline-confirm" role="group" aria-label={t("remote.host.deleteConfirm")}>
                <span>{t("remote.host.deleteConfirm")}</span>
                <button className="btn btn--small btn--danger" type="button" disabled={busy} onClick={deleteHost}>{t("common.confirm")}</button>
                <button className="btn btn--small" type="button" disabled={busy} onClick={() => setConfirmDelete(false)}>{t("common.cancel")}</button>
              </div>
            )}
          </div>
        )}
      </section>

      <section className="settings-section remote-host-editor">
        <div className="settings-section__title">{editingId ? t("remote.host.edit") : t("remote.host.add")}</div>
        <label className="remote-host-editor__field">
          <span>{t("remote.host.alias")}</span>
          <input value={alias} disabled={busy} autoComplete="off" spellCheck={false} onInput={(event) => setAlias(event.currentTarget.value)} placeholder="reasonix-host" />
          <small>{t("remote.host.aliasHint")}</small>
        </label>
        <label className="remote-host-editor__field">
          <span>{t("remote.host.label")}</span>
          <input value={label} disabled={busy} autoComplete="off" onInput={(event) => setLabel(event.currentTarget.value)} placeholder={t("remote.host.labelPlaceholder")} />
        </label>
        <label className="remote-host-editor__field">
          <span>{t("remote.host.sshConfig")}</span>
          <input value={sshConfigPath} disabled={busy} autoComplete="off" spellCheck={false} onInput={(event) => setSSHConfigPath(event.currentTarget.value)} placeholder={t("remote.host.sshConfigPlaceholder")} />
          <small>{t("remote.host.sshConfigHint")}</small>
        </label>
        <div className="remote-host-editor__actions">
          <button className="btn btn--primary" type="button" disabled={busy || !alias.trim() || !label.trim()} onClick={() => void saveHost()}>{t("common.save")}</button>
          {editingId && <button className="btn" type="button" disabled={busy} onClick={startAdd}>{t("common.cancel")}</button>}
        </div>
      </section>
    </div>
  );
}

function RemoteHostRuntimeSection({
  summary,
  loading,
  error,
  onRefresh,
}: {
  summary: RemoteHostRuntimeSummaryView | null;
  loading: boolean;
  error: string;
  onRefresh: () => Promise<void>;
}) {
  const t = useT();
  const core = summary ? [
    ["coreSession", summary.capabilities.features.coreSession],
    ["primaryFileQueries", summary.capabilities.features.primaryFileQueries],
    ["userShell", summary.capabilities.features.userShell],
    ["jobCancel", summary.capabilities.features.jobCancel],
    ["memory", summary.capabilities.features.memory],
    ["research", summary.capabilities.features.research],
  ] as const : [];
  const deferred = summary ? [
    ["attachments", summary.capabilities.features.attachments],
    ["clipboardImages", summary.capabilities.features.clipboardImages],
    ["sftp", summary.capabilities.features.sftp],
    ["pty", summary.capabilities.features.pty],
    ["gitWrite", summary.capabilities.features.gitWrite],
    ["deliveryWorktree", summary.capabilities.features.deliveryWorktree],
  ] as const : [];
  const config = summary?.config;
  const catalog = summary?.catalog;
  return (
    <section className="settings-section remote-host-runtime" data-testid="remote-host-runtime-summary">
      <div className="settings-section__head">
        <div>
          <div className="settings-section__title">{t("remote.runtime.title")}</div>
          <div className="settings-section__desc">{t("remote.runtime.description")}</div>
        </div>
        <button className="btn btn--small" type="button" disabled={loading} onClick={() => void onRefresh()}>
          {loading ? <Loader2 className="spin" size={14} aria-hidden="true" /> : <RefreshCw size={14} aria-hidden="true" />}
          {t("remote.runtime.refresh")}
        </button>
      </div>
      {error && <div className="banner banner--error" role="alert">{error}</div>}
      {loading && !summary ? (
        <div className="empty"><Loader2 className="spin" size={16} aria-hidden="true" /> {t("settings.loading")}</div>
      ) : summary ? (
        <div className="remote-host-runtime__body">
          <div className="remote-host-runtime__group">
            <h3>{t("remote.runtime.capabilities")}</h3>
            <ul className="remote-host-runtime__capabilities">
              {core.map(([name, available]) => (
                <li key={name}><span>{t(`remote.runtime.feature.${name}` as const)}</span><strong data-available={available}>{t(available ? "remote.runtime.available" : "remote.runtime.unavailable")}</strong></li>
              ))}
            </ul>
          </div>
          <div className="remote-host-runtime__group">
            <h3>{t("remote.runtime.deferred")}</h3>
            <p>{t("remote.runtime.deferredHint")}</p>
            <ul className="remote-host-runtime__capabilities">
              {deferred.map(([name, available]) => (
                <li key={name}><span>{t(`remote.runtime.feature.${name}` as const)}</span><strong data-available={available}>{t(available ? "remote.runtime.available" : "remote.runtime.unavailable")}</strong></li>
              ))}
            </ul>
          </div>
          <div className="remote-host-runtime__group remote-host-runtime__config">
            <h3>{t("remote.runtime.config")}</h3>
            {!config?.available ? (
              <div className="banner banner--warning">{config?.unavailableReason || t("remote.runtime.configUnavailable")}</div>
            ) : (
              <>
                <ul className="remote-host-runtime__feature-states">
                  {config.featureStates.length === 0 ? <li>{t("remote.runtime.noFeatureStates")}</li> : config.featureStates.map((feature) => (
                    <li key={feature.feature}><strong>{feature.feature}</strong><span>{feature.summary || t(feature.available ? "remote.runtime.available" : "remote.runtime.unavailable")}</span></li>
                  ))}
                </ul>
                {config.displayPaths.length > 0 && <dl className="remote-host-runtime__paths">
                  {config.displayPaths.map((path) => <div key={`${path.scope}:${path.displayPath}`}><dt>{path.scope}</dt><dd>{path.displayPath}</dd></div>)}
                </dl>}
                {config.cliHints.length > 0 && <div className="remote-host-runtime__hints">
                  {config.cliHints.map((hint) => <div key={`${hint.label}:${hint.command}`}><span>{hint.label}</span><code>{hint.command}</code></div>)}
                </div>}
              </>
            )}
          </div>
          <div className="remote-host-runtime__group remote-host-runtime__catalog" data-testid="remote-session-catalog">
            <h3>{t("remote.runtime.catalog")}</h3>
            <p>{t("remote.runtime.catalogHint")}</p>
            {!catalog?.available ? (
              <div className="banner banner--warning">{t("remote.runtime.catalogUnavailable")}</div>
            ) : (
              <div className="remote-session-catalog">
                <section aria-label={t("remote.runtime.catalogMCP")}>
                  <h4>{t("remote.runtime.catalogMCP")}</h4>
                  <ul>
                    {catalog.mcpServers.length === 0 ? <li className="remote-session-catalog__empty">{t("remote.runtime.catalogEmpty")}</li> : catalog.mcpServers.map((server) => (
                      <li key={server.name}>
                        <span><strong>{server.name}</strong><small>{t("remote.runtime.catalogToolCount", { count: server.toolCount })}</small></span>
                        <em data-available={server.status === "available"}>{t(server.status === "available" ? "remote.runtime.available" : "remote.runtime.unavailable")}</em>
                      </li>
                    ))}
                  </ul>
                </section>
                <section aria-label={t("remote.runtime.catalogSkills")}>
                  <h4>{t("remote.runtime.catalogSkills")}</h4>
                  <ul>
                    {catalog.skills.length === 0 ? <li className="remote-session-catalog__empty">{t("remote.runtime.catalogEmpty")}</li> : catalog.skills.map((skill) => (
                      <li key={`${skill.scope}:${skill.name}`}>
                        <span><strong>{skill.name}</strong>{skill.description && <small>{skill.description}</small>}</span>
                        <em>{skill.scope}</em>
                      </li>
                    ))}
                  </ul>
                </section>
                <section aria-label={t("remote.runtime.catalogPlugins")}>
                  <h4>{t("remote.runtime.catalogPlugins")}</h4>
                  <ul>
                    {catalog.plugins.length === 0 ? <li className="remote-session-catalog__empty">{t("remote.runtime.catalogEmpty")}</li> : catalog.plugins.map((plugin) => (
                      <li key={plugin.name}>
                        <strong>{plugin.name}</strong>
                        <em data-available={plugin.enabled}>{t(plugin.enabled ? "remote.runtime.catalogEnabled" : "remote.runtime.catalogDisabled")}</em>
                      </li>
                    ))}
                  </ul>
                </section>
              </div>
            )}
          </div>
        </div>
      ) : null}
    </section>
  );
}

export default RemoteSettingsPage;
