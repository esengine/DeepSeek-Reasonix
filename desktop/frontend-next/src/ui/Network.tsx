import { useEffect, useRef, useState } from "react";
import { t } from "../i18n";
import type { AgentPort, NetworkProbe, NetworkSettings } from "../port/port";
import { reason, say } from "../i18n/kernel";

// Three modes, not six input boxes. Nobody's first question is whether they want
// http_proxy or https_proxy — it is "use what the system uses" or "here is mine".
const MODES: [string, string, string][] = [
  ["auto", "common.followTheSystem", "network.usesWhateverProxyTheSystem"],
  ["custom", "network.setManually", "network.useTheProxyYouSpecify"],
  ["off", "common.direct", "network.useNoProxy"],
];

const STEP_LABEL: Record<string, string> = {
  proxy: "common.proxy",
  dns: "network.nameResolution",
  connect: "network.connection",
  tls: "network.tLSHandshake",
  auth: "network.credentials",
};

const TYPES = ["http", "https", "socks5", "socks5h"];
const EDITABLE: (keyof NetworkSettings)[] = ["mode", "type", "server", "port", "username", "noProxy"];

export function Network({ port, host = "" }: { port: AgentPort; host?: string }) {
  const [connection, setConnection] = useState({ port, generation: 0 });
  if (connection.port !== port) setConnection({ port, generation: connection.generation + 1 });
  return <NetworkInput key={connection.generation} port={port} host={host} />;
}

function NetworkInput({ port, host }: { port: AgentPort; host: string }) {
  const [settings, setSettings] = useState<{ saved: NetworkSettings; draft: NetworkSettings } | null>(null);
  const [password, setPassword] = useState("");
  const [clearPassword, setClear] = useState(false);
  const [probes, setProbes] = useState<NetworkProbe[] | null>(null);
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");
  const live = useRef(false);
  const pending = useRef<"" | "save" | "test">("");

  useEffect(() => {
    let current = true;
    live.current = true;
    port.network().then((net) => current && setSettings({ saved: net, draft: net }))
      .catch((e) => current && setError(reason(e)));
    return () => { current = false; live.current = false; };
  }, [port]);

  if (!settings) return <div className="empty" role={error ? "alert" : "status"}>{error || t("network.loadingNetworkSettings")}</div>;
  const { saved, draft: net } = settings;
  const changed = EDITABLE.some((key) => net[key] !== saved[key]) || password !== "" || clearPassword;

  const patch = (p: Partial<NetworkSettings>) => {
    if (!live.current || pending.current === "save") return;
    setSettings((current) => current && { ...current, draft: { ...current.draft, ...p } });
  };

  const save = async () => {
    if (!live.current || pending.current || !changed) return;
    pending.current = "save";
    setBusy("save");
    setError("");
    try {
      const result = await port.saveNetwork(net, password, clearPassword);
      if (!live.current) return;
      setSettings({ saved: result, draft: result });
      setPassword("");
      setClear(false);
      // The old result described the old settings; keeping it on screen would
      // claim a path that has not been tested since.
      setProbes(null);
    } catch (e) {
      if (live.current) setError(reason(e));
    } finally {
      pending.current = "";
      if (live.current) setBusy("");
    }
  };

  const diagnose = async () => {
    if (!live.current || pending.current) return;
    pending.current = "test";
    setBusy("test");
    setError("");
    try {
      const result = await port.diagnoseNetwork();
      if (live.current) setProbes(result);
    } catch (e) {
      if (live.current) setError(reason(e));
    } finally {
      pending.current = "";
      if (live.current) setBusy("");
    }
  };

  return (
    <div className="net">
      {host && (
        <p className="note">{t("network.theseAreTheNetworkSettings", { host })}</p>
      )}
      <div className="seg" data-text role="radiogroup" aria-label={t("network.proxyMode")}>
        {MODES.map(([id, label]) => (
          <button key={id} role="radio" aria-checked={net.mode === id} disabled={busy === "save"} onClick={() => patch({ mode: id })}>
            {t(label)}
          </button>
        ))}
      </div>
      <p className="note">{t(MODES.find(([id]) => id === net.mode)?.[2] ?? "")}</p>

      <div className="kv">
        <span className="k">{t("common.inEffect")}</span>
        <span className="v">{saved.effective || "—"}</span>
      </div>
      {saved.endpoint && (
        <div className="kv">
          <span className="k">{t("network.testTarget")}</span>
          <span className="v">{saved.endpoint}</span>
        </div>
      )}
      {!!saved.direct?.length && (
        <div className="kv">
          <span className="k">{t("network.alwaysConnectDirectly")}</span>
          <span className="v">{saved.direct.join(", ")}</span>
        </div>
      )}

      {net.mode === "custom" && (
        <div className="fields">
          <label>
            <span>{t("common.protocol2")}</span>
            <select value={net.type || "http"} disabled={busy === "save"} onChange={(e) => patch({ type: e.target.value })}>
              {TYPES.map((t) => (
                <option key={t} value={t}>
                  {t}
                </option>
              ))}
            </select>
          </label>
          <label className="grow">
            <span>{t("common.server")}</span>
            <input value={net.server ?? ""} disabled={busy === "save"} placeholder="proxy.corp" onChange={(e) => patch({ server: e.target.value })} />
          </label>
          <label>
            <span>{t("common.port")}</span>
            <input
              className="port"
              inputMode="numeric"
              disabled={busy === "save"}
              value={net.port ? String(net.port) : ""}
              placeholder="8080"
              onChange={(e) => patch({ port: Number(e.target.value.replace(/\D/g, "")) || 0 })}
            />
          </label>
          <label className="grow">
            <span>{t("network.usernameOptional")}</span>
            <input value={net.username ?? ""} disabled={busy === "save"} onChange={(e) => patch({ username: e.target.value })} />
          </label>
          <label className="grow">
            <span>{t("network.passwordOptional")}</span>
            <input
              type="password"
              disabled={busy === "save"}
              value={password}
              placeholder={t(net.hasPassword ? "network.savedLeaveBlankToKeep" : "network.youCanWrite")}
              onChange={(e) => {
                if (!live.current || pending.current === "save") return;
                setPassword(e.target.value);
                setClear(false);
              }}
            />
          </label>
          {net.hasPassword && !password && (
            <label className="chk">
              <input type="checkbox" checked={clearPassword} disabled={busy === "save"} onChange={(e) => {
                if (live.current && pending.current !== "save") setClear(e.target.checked);
              }} />
              <span>{t("network.deleteTheSavedPassword")}</span>
            </label>
          )}
          <label className="grow full">
            <span>{t("network.reachTheseAddressesDirectly")}</span>
            <input
              value={net.noProxy ?? ""}
              disabled={busy === "save"}
              placeholder="localhost, 127.0.0.1, *.internal"
              onChange={(e) => patch({ noProxy: e.target.value })}
            />
          </label>
        </div>
      )}

      <div className="acts">
        <button className="act" data-action="network.diagnose" disabled={!!busy} onClick={() => void diagnose()}>
          {t(busy === "test" ? "common.testing" : "network.testSavedSettings")}
        </button>
        <button className="act" data-action="network.save" data-primary disabled={!!busy || !changed} onClick={() => void save()}>
          {t(busy === "save" ? "common.saving2" : "common.save")}
        </button>
      </div>
      {changed && <p className="note" role="status">{t("network.thereAreUnsavedChangesConnection")}</p>}

      {probes && (
        <div className="probes" role="region" aria-label={t("network.testResultsForSavedSettings")}>
          {probes.map((p, i) => (
            <div className="probe" key={i} data-ok={p.ok ? "" : undefined}>
              <i className="mk">{p.ok ? "✓" : "×"}</i>
              <span className="lb">{STEP_LABEL[p.step] ? t(STEP_LABEL[p.step]) : p.step}</span>
              <span className="dt">{say(p.code ? { code: p.code, params: p.params } : null, p.detail)}</span>
              <span className="ms">{p.durationMs ? `${p.durationMs}ms` : ""}</span>
              {p.advice && <span className="adv">{say(p.adviceCode ? { code: p.adviceCode, params: p.params } : null, p.advice)}</span>}
            </div>
          ))}
        </div>
      )}
      {error && <div className="why">{error}</div>}
    </div>
  );
}
