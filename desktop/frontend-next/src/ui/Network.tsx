import { useEffect, useRef, useState } from "react";
import { t } from "../i18n";
import type { AgentPort, NetworkProbe, NetworkSettings } from "../port/port";
import { reason, say } from "../i18n/kernel";

// Three modes, not six input boxes. Nobody's first question is whether they want
// http_proxy or https_proxy — it is "use what the system uses" or "here is mine".
const MODES: [string, string, string][] = [
  ["auto", "跟随系统", "使用系统或环境变量中已配置的代理"],
  ["custom", "手动设置", "手动指定代理服务器"],
  ["off", "直连", "不使用任何代理"],
];

const STEP_LABEL: Record<string, string> = {
  proxy: "代理",
  dns: "域名解析",
  connect: "建立连接",
  tls: "加密握手",
  auth: "凭据",
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

  if (!settings) return <div className="empty" role={error ? "alert" : "status"}>{error || t("正在读取网络配置…")}</div>;
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
        <p className="note">{t("以下是 {host} 上的网络设置。本机的代理需要在打开本机会话时设置。", { host })}</p>
      )}
      <div className="seg" data-text role="radiogroup" aria-label={t("代理模式")}>
        {MODES.map(([id, label]) => (
          <button key={id} role="radio" aria-checked={net.mode === id} disabled={busy === "save"} onClick={() => patch({ mode: id })}>
            {t(label)}
          </button>
        ))}
      </div>
      <p className="note">{t(MODES.find(([id]) => id === net.mode)?.[2] ?? "")}</p>

      <div className="kv">
        <span className="k">{t("当前生效")}</span>
        <span className="v">{saved.effective || "—"}</span>
      </div>
      {saved.endpoint && (
        <div className="kv">
          <span className="k">{t("测试目标")}</span>
          <span className="v">{saved.endpoint}</span>
        </div>
      )}
      {!!saved.direct?.length && (
        <div className="kv">
          <span className="k">{t("始终直连")}</span>
          <span className="v">{saved.direct.join(", ")}</span>
        </div>
      )}

      {net.mode === "custom" && (
        <div className="fields">
          <label>
            <span>{t("协议")}</span>
            <select value={net.type || "http"} disabled={busy === "save"} onChange={(e) => patch({ type: e.target.value })}>
              {TYPES.map((t) => (
                <option key={t} value={t}>
                  {t}
                </option>
              ))}
            </select>
          </label>
          <label className="grow">
            <span>{t("服务器")}</span>
            <input value={net.server ?? ""} disabled={busy === "save"} placeholder="proxy.corp" onChange={(e) => patch({ server: e.target.value })} />
          </label>
          <label>
            <span>{t("端口")}</span>
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
            <span>{t("用户名（可选）")}</span>
            <input value={net.username ?? ""} disabled={busy === "save"} onChange={(e) => patch({ username: e.target.value })} />
          </label>
          <label className="grow">
            <span>{t("密码（可选）")}</span>
            <input
              type="password"
              disabled={busy === "save"}
              value={password}
              placeholder={t(net.hasPassword ? "已保存，留空即保持不变" : "可填写 ${PROXY_PASSWORD}")}
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
              <span>{t("删除已保存的密码")}</span>
            </label>
          )}
          <label className="grow full">
            <span>{t("这些地址直连")}</span>
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
          {t(busy === "test" ? "测试中…" : "测试已保存配置")}
        </button>
        <button className="act" data-action="network.save" data-primary disabled={!!busy || !changed} onClick={() => void save()}>
          {t(busy === "save" ? "保存中…" : "保存")}
        </button>
      </div>
      {changed && <p className="note" role="status">{t("有未保存的改动；连接测试使用已保存的配置。")}</p>}

      {probes && (
        <div className="probes" role="region" aria-label={t("已保存配置的测试结果")}>
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
