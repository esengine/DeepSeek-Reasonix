import { useEffect, useState } from "react";
import { app } from "../lib/bridge";
import { useT } from "../lib/i18n";
import type { CapabilitiesView, MCPServerInput, ServerView } from "../lib/types";

// CapabilitiesPanel is the desktop MCP & Skills drawer — the GUI counterpart to
// the CLI's /mcp + /skill, aligning with Claude Code's Customize → Connectors:
// each server shows a connected/failed dot, transport, and tool/prompt/resource
// counts, with add / remove / retry; skills list their scope and run mode.
export function CapabilitiesPanel({ onClose }: { onClose: () => void }) {
  const t = useT();
  const [view, setView] = useState<CapabilitiesView | null>(null);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const [adding, setAdding] = useState(false);
  const [confirming, setConfirming] = useState<string | null>(null);

  const reload = async () =>
    setView(await app.Capabilities().catch(() => ({ servers: [], skills: [] })));
  useEffect(() => {
    void reload();
  }, []);

  // mutate runs an MCP edit, re-reads the snapshot, and surfaces any failure as an
  // inline banner (a connect error, a missing binary, a bad URL).
  const mutate = async (fn: () => Promise<unknown>) => {
    setBusy(true);
    setErr(null);
    try {
      await fn();
      await reload();
      return true;
    } catch (e) {
      setErr(String((e as Error)?.message ?? e));
      return false;
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="drawer-backdrop" onClick={onClose}>
      <aside className="drawer" onClick={(e) => e.stopPropagation()}>
        <header className="drawer__head">
          <div className="drawer__title">{t("caps.title")}</div>
          <button className="chip" onClick={onClose} title={t("common.close")}>
            ✕
          </button>
        </header>

        {!view ? (
          <div className="empty">{t("caps.loading")}</div>
        ) : (
          <div className="drawer__body">
            {err && <div className="banner banner--error">{err}</div>}

            <section className="mem-section">
              <div className="mem-section__title">{t("caps.servers")}</div>
              {view.servers.length === 0 && !adding && (
                <div className="mem-empty">{t("caps.noServers")}</div>
              )}
              {view.servers.map((s) => (
                <ServerRow
                  key={s.name}
                  s={s}
                  busy={busy}
                  confirming={confirming === s.name}
                  onConfirm={() => setConfirming(s.name)}
                  onCancelConfirm={() => setConfirming(null)}
                  onRemove={() => mutate(() => app.RemoveMCPServer(s.name)).then(() => setConfirming(null))}
                  onRetry={() => void mutate(() => app.RetryMCPServer(s.name))}
                  onToggle={(on) => void mutate(() => app.SetMCPServerEnabled(s.name, on))}
                />
              ))}
              {adding ? (
                <AddServerForm busy={busy} onCancel={() => setAdding(false)} onAdd={async (input) => (await mutate(() => app.AddMCPServer(input))) && setAdding(false)} />
              ) : (
                <button className="btn btn--small" disabled={busy} onClick={() => setAdding(true)}>
                  {t("caps.addServer")}
                </button>
              )}
            </section>

            <section className="mem-section">
              <div className="mem-section__title">{t("caps.skills")}</div>
              {view.skills.length === 0 ? (
                <div className="mem-empty">{t("caps.noSkills")}</div>
              ) : (
                view.skills.map((sk) => (
                  <div className="cap-row" key={sk.name}>
                    <span className="cap-slash">/</span>
                    <div className="cap-row__text">
                      <div className="cap-row__head">
                        <span className="cap-row__name">{sk.name}</span>
                        <span className={`badge badge--${sk.scope}`}>{sk.scope}</span>
                        {sk.runAs === "subagent" && <span className="badge">{t("caps.subagent")}</span>}
                      </div>
                      <div className="cap-row__sub">{sk.description}</div>
                    </div>
                  </div>
                ))
              )}
            </section>
          </div>
        )}
      </aside>
    </div>
  );
}

function ServerRow({
  s,
  busy,
  confirming,
  onConfirm,
  onCancelConfirm,
  onRemove,
  onRetry,
  onToggle,
}: {
  s: ServerView;
  busy: boolean;
  confirming: boolean;
  onConfirm: () => void;
  onCancelConfirm: () => void;
  onRemove: () => void;
  onRetry: () => void;
  onToggle: (on: boolean) => void;
}) {
  const t = useT();
  const sub =
    s.status === "failed"
      ? s.error || t("caps.failed")
      : s.status === "disabled"
        ? t("caps.disabled")
        : t("caps.counts", { tools: s.tools, prompts: s.prompts, resources: s.resources });
  return (
    <div className="cap-row" title={s.error || undefined}>
      <span className={`cap-dot cap-dot--${s.status}`} />
      <div className="cap-row__text">
        <div className="cap-row__head">
          <span className="cap-row__name">{s.name}</span>
          <span className="cap-row__transport">{s.transport}</span>
        </div>
        <div className="cap-row__sub">{sub}</div>
      </div>
      <div className="cap-row__actions">
        {confirming ? (
          <>
            <button className="btn btn--small" disabled={busy} onClick={onRemove}>
              {t("caps.confirmRemove")}
            </button>
            <button className="btn btn--small" disabled={busy} onClick={onCancelConfirm}>
              {t("common.cancel")}
            </button>
          </>
        ) : (
          <>
            {s.status === "failed" ? (
              <button className="btn btn--small" disabled={busy} onClick={onRetry}>
                {t("caps.retry")}
              </button>
            ) : (
              <label className="cap-switch" title={s.status === "connected" ? t("caps.disable") : t("caps.enable")}>
                <input
                  type="checkbox"
                  checked={s.status === "connected"}
                  disabled={busy}
                  onChange={(e) => onToggle(e.target.checked)}
                />
                <span className="cap-switch__track" />
              </label>
            )}
            <button className="btn btn--small" disabled={busy} onClick={onConfirm} title={t("caps.remove")}>
              ✕
            </button>
          </>
        )}
      </div>
    </div>
  );
}

function AddServerForm({
  busy,
  onCancel,
  onAdd,
}: {
  busy: boolean;
  onCancel: () => void;
  onAdd: (input: MCPServerInput) => void;
}) {
  const t = useT();
  const [name, setName] = useState("");
  const [transport, setTransport] = useState("stdio");
  const [command, setCommand] = useState("");
  const [url, setUrl] = useState("");
  const [env, setEnv] = useState("");

  const isStdio = transport === "stdio";
  const ready = name.trim() !== "" && (isStdio ? command.trim() !== "" : url.trim() !== "");

  const submit = () => {
    const parts = command.trim().split(/\s+/).filter(Boolean);
    const envMap: Record<string, string> = {};
    for (const line of env.split("\n")) {
      const eq = line.indexOf("=");
      if (eq > 0) envMap[line.slice(0, eq).trim()] = line.slice(eq + 1).trim();
    }
    onAdd({
      name: name.trim(),
      transport,
      command: isStdio ? (parts[0] ?? "") : "",
      args: isStdio ? parts.slice(1) : [],
      url: isStdio ? "" : url.trim(),
      env: envMap,
    });
  };

  return (
    <div className="prov-card prov-card--edit">
      <input className="mem-input" placeholder={t("caps.namePlaceholder")} value={name} onChange={(e) => setName(e.target.value)} />
      <label className="set-label">{t("caps.transport")}</label>
      <select className="mem-select" value={transport} onChange={(e) => setTransport(e.target.value)}>
        <option value="stdio">stdio</option>
        <option value="http">http</option>
        <option value="sse">sse</option>
      </select>
      {isStdio ? (
        <input className="mem-input" placeholder={t("caps.commandPlaceholder")} value={command} onChange={(e) => setCommand(e.target.value)} />
      ) : (
        <input className="mem-input" placeholder={t("caps.urlPlaceholder")} value={url} onChange={(e) => setUrl(e.target.value)} />
      )}
      <label className="set-label">{t("caps.envLabel")}</label>
      <textarea className="mem-textarea" value={env} onChange={(e) => setEnv(e.target.value)} placeholder={t("caps.envPlaceholder")} spellCheck={false} />
      <div className="prov-card__actions">
        <button className="btn btn--small" onClick={onCancel} disabled={busy}>
          {t("common.cancel")}
        </button>
        <button className="btn btn--primary btn--small" onClick={submit} disabled={busy || !ready}>
          {t("caps.add")}
        </button>
      </div>
    </div>
  );
}
