import { useEffect, useState } from "react";
import { t } from "../i18n";
import type { AgentPort, NetworkProbe, NetworkSettings } from "../port/port";
import { reason } from "../i18n/kernel";

// Three modes, not six input boxes. Nobody's first question is whether they want
// http_proxy or https_proxy — it is "use what the system uses" or "here is mine".
const MODES: [string, string, string][] = [
  ["auto", "跟随系统", "用系统或环境变量里已经设好的代理"],
  ["custom", "手动设置", "自己指定一个代理服务器"],
  ["off", "直连", "谁的代理都不走"],
];

const STEP_LABEL: Record<string, string> = {
  proxy: "代理",
  dns: "域名解析",
  connect: "建立连接",
  tls: "加密握手",
  auth: "凭据",
};

const TYPES = ["http", "https", "socks5", "socks5h"];

export function Network({ port }: { port: AgentPort }) {
  const [net, setNet] = useState<NetworkSettings | null>(null);
  const [password, setPassword] = useState("");
  const [clearPassword, setClear] = useState(false);
  const [probes, setProbes] = useState<NetworkProbe[] | null>(null);
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");

  useEffect(() => {
    port.network().then(setNet).catch(() => setNet(null));
  }, [port]);

  if (!net) return <div className="empty">{t("读不到网络配置。")}</div>;

  const patch = (p: Partial<NetworkSettings>) => setNet({ ...net, ...p });

  const save = async () => {
    setBusy("save");
    setError("");
    try {
      setNet(await port.saveNetwork(net, password, clearPassword));
      setPassword("");
      setClear(false);
      // The old result described the old settings; keeping it on screen would
      // claim a path that has not been tested since.
      setProbes(null);
    } catch (e) {
      setError(reason(e));
    } finally {
      setBusy("");
    }
  };

  const diagnose = async () => {
    setBusy("test");
    setError("");
    try {
      setProbes(await port.diagnoseNetwork());
    } catch (e) {
      setError(reason(e));
    } finally {
      setBusy("");
    }
  };

  return (
    <div className="net">
      <div className="seg" data-text role="radiogroup" aria-label={t("代理模式")}>
        {MODES.map(([id, label]) => (
          <button key={id} role="radio" aria-checked={net.mode === id} onClick={() => patch({ mode: id })}>
            {t(label)}
          </button>
        ))}
      </div>
      <p className="note">{t(MODES.find(([id]) => id === net.mode)?.[2] ?? "")}</p>

      <div className="kv">
        <span className="k">{t("当前生效")}</span>
        <span className="v">{net.effective || "—"}</span>
      </div>
      {net.endpoint && (
        <div className="kv">
          <span className="k">{t("测试目标")}</span>
          <span className="v">{net.endpoint}</span>
        </div>
      )}
      {!!net.direct?.length && (
        <div className="kv">
          <span className="k">{t("始终直连")}</span>
          <span className="v">{net.direct.join(", ")}</span>
        </div>
      )}

      {net.mode === "custom" && (
        <div className="fields">
          <label>
            <span>{t("协议")}</span>
            <select value={net.type || "http"} onChange={(e) => patch({ type: e.target.value })}>
              {TYPES.map((t) => (
                <option key={t} value={t}>
                  {t}
                </option>
              ))}
            </select>
          </label>
          <label className="grow">
            <span>{t("服务器")}</span>
            <input value={net.server ?? ""} placeholder="proxy.corp" onChange={(e) => patch({ server: e.target.value })} />
          </label>
          <label>
            <span>{t("端口")}</span>
            <input
              className="port"
              inputMode="numeric"
              value={net.port ? String(net.port) : ""}
              placeholder="8080"
              onChange={(e) => patch({ port: Number(e.target.value.replace(/\D/g, "")) || 0 })}
            />
          </label>
          <label className="grow">
            <span>{t("用户名（可选）")}</span>
            <input value={net.username ?? ""} onChange={(e) => patch({ username: e.target.value })} />
          </label>
          <label className="grow">
            <span>{t("密码（可选）")}</span>
            <input
              type="password"
              value={password}
              placeholder={t(net.hasPassword ? "已保存，留空即保持不变" : "可以写 ${PROXY_PASSWORD}")}
              onChange={(e) => {
                setPassword(e.target.value);
                setClear(false);
              }}
            />
          </label>
          {net.hasPassword && !password && (
            <label className="chk">
              <input type="checkbox" checked={clearPassword} onChange={(e) => setClear(e.target.checked)} />
              <span>{t("删掉已保存的密码")}</span>
            </label>
          )}
          <label className="grow full">
            <span>{t("这些地址直连")}</span>
            <input
              value={net.noProxy ?? ""}
              placeholder="localhost, 127.0.0.1, *.internal"
              onChange={(e) => patch({ noProxy: e.target.value })}
            />
          </label>
        </div>
      )}

      <div className="acts">
        <button className="act" disabled={!!busy} onClick={() => void diagnose()}>
          {t(busy === "test" ? "测试中…" : "测一下")}
        </button>
        <button className="act" data-primary disabled={!!busy} onClick={() => void save()}>
          {t(busy === "save" ? "保存中…" : "保存")}
        </button>
      </div>

      {probes && (
        <div className="probes">
          {probes.map((p, i) => (
            <div className="probe" key={i} data-ok={p.ok ? "" : undefined}>
              <i className="mk">{p.ok ? "✓" : "×"}</i>
              <span className="lb">{STEP_LABEL[p.step] ?? p.step}</span>
              <span className="dt">{p.detail}</span>
              <span className="ms">{p.durationMs ? `${p.durationMs}ms` : ""}</span>
              {p.advice && <span className="adv">{p.advice}</span>}
            </div>
          ))}
        </div>
      )}
      {error && <div className="why">{error}</div>}
    </div>
  );
}
