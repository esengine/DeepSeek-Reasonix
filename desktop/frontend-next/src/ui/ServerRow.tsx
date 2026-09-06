import { useState } from "react";
import { t } from "../i18n";
import type { AgentPort, McpEntry } from "../port/port";
import { Exception } from "./CapabilityScope";
import { Switch } from "./Switch";
import { reason } from "../i18n/kernel";

// One external service: whether it is answering, what it brought, and the two
// acts that change that — switching it off and letting it try again.
// standby is the ordinary state of a working server, not a lesser kind of
// connected: its tools are in the catalog and the process starts on the first
// call. Calling that "not connected" beside a Reconnect button reads as broken.
const MCP_STATE: Record<string, string> = {
  ready: "已连接",
  connecting: "连接中",
  failed: "连不上",
  disabled: "已关闭",
  standby: "待命 · 首次调用时启动",
  idle: "未连接",
};

// The tool list is the long half and the least urgent, so it folds behind its
// own count — the same idiom a long file read uses in the transcript.
export function ServerRow({
  m, port, onDone, root, live,
}: {
  m: McpEntry; port: AgentPort; onDone: () => void; root: string; live: boolean;
}) {
  const [busy, setBusy] = useState("");
  const [failed, setFailed] = useState("");
  const [confirming, setConfirming] = useState(false);
  // 401/403 is not a broken server, it is a server that stopped trusting this
  // machine — retrying without saying so sends the user around the same loop.
  const auth = /\b(401|403|unauthorized|forbidden|auth)/i.test(m.error ?? failed);
  const meta = [t(MCP_STATE[m.state] ?? m.state), m.transport, m.source].filter(Boolean).join(" · ");

  const run = async (what: string, fn: () => Promise<unknown>) => {
    setBusy(what);
    setFailed("");
    try {
      const r = (await fn()) as { error?: string } | void;
      if (r && typeof r === "object" && r.error) setFailed(r.error);
    } catch (e) {
      setFailed(reason(e));
    } finally {
      setBusy("");
      onDone();
    }
  };

  const actions = (
    <span className="acts">
      {live && m.enabled && m.state !== "ready" && (
        <button className="act" data-action="mcp.retry" data-target={m.name} disabled={!!busy} onClick={() => void run("retry", () => port.reconnectMcp(m.name))}>
          {t(busy === "retry" ? "连接中…" : auth ? "重新授权" : m.state === "standby" ? "立即连接" : "重连")}
        </button>
      )}
      {/* Removal is the one action here that cannot be undone by clicking again,
          so it asks — and the question names the file it is about to edit. */}
      {live && (
        <button className="act ghost" data-action="mcp.remove" data-target={m.name} aria-label={t("移除 {name}", { name: m.name })} disabled={!!busy} onClick={() => setConfirming(true)}>
          {t("移除")}
        </button>
      )}
      <Switch
        data-action="mcp.enabled"
        data-target={m.name}
        on={m.enabled}
        busy={busy === "toggle"}
        label={t(m.enabled ? "关闭 {name}" : "启用 {name}", { name: m.name })}
        onClick={() => void run("toggle", () => port.setMcpEnabled(m.name, !m.enabled, "project", root || undefined))}
      />
    </span>
  );

  const confirm = confirming && (
    <div className="confirm">
      <span className="q">
        {t("把 {name} 从 {where} 里删掉？只是想暂时不用的话，关掉开关就够了。", { name: m.name, where: m.source || t("配置") })}
      </span>
      <button className="act" data-action="mcp.remove" data-target={m.name} data-value="cancel" onClick={() => setConfirming(false)}>
        {t("算了")}
      </button>
      <button
        className="act danger"
        data-action="mcp.remove"
        data-target={m.name}
        data-value="confirm"
        disabled={busy === "remove"}
        onClick={() =>
          void run("remove", async () => {
            const r = await port.removeMcp(m.name);
            setConfirming(false);
            // A lower-precedence declaration with the same name may have taken
            // over; saying so beats a list that looks like the delete failed.
            if (r.stillConfigured) setFailed(t("同名的另一处声明现在生效了，这一行不会消失。"));
          })
        }
      >
        {t(busy === "remove" ? "移除中…" : "移除")}
      </button>
    </div>
  );

  const tools = m.toolList ?? [];
  const head = (
    <>
      <i className="pip" />
      <span className="nm">{m.name}</span>
      {tools.length ? <span className="fold">{t("{n} 个工具", { n: m.tools })}</span> : null}
      <span className="meta">{meta}</span>
      {m.localOverride && <Exception onClear={() => void run("clear", () => port.clearMcpOverride(m.name, root || undefined))} busy={busy === "clear"} />}
      {actions}
    </>
  );
  // 服务是干什么的，只有服务自己说了算：MCP 握手里的那段自述。它没写，这里就
  // 没有 —— 拿名字或配置凑一句出来，等于替它编。
  const about = (!!m.description || (!tools.length && m.state !== "connecting")) && (
    <div className="srv-ab">
      <span className="ds">{m.description || t("这个服务没写自我说明。")}</span>
      {m.remembered && (
        <i className="w" title={t("现在没连着，这是上一次连上时它自己给的答复。")}>
          {t(m.stale ? "上次连上时的记录 · 声明改过，可能对不上了" : "上次连上时的记录")}
        </i>
      )}
    </div>
  );
  const why = m.error || failed;
  if (!tools.length) {
    return (
      <div className="srv" data-st={m.state} data-local={m.localOverride ? "" : undefined}>
        <div className="srv-hd">{head}</div>
        {about}
        {why && <div className="why">{why}</div>}
        {confirm}
      </div>
    );
  }
  return (
    // Asking to remove has to open the row: the confirmation lives inside the
    // fold, and what the server contributes is worth seeing before dropping it.
    <details className="srv" data-st={m.state} data-local={m.localOverride ? "" : undefined} open={confirming || undefined}>
      <summary>{head}</summary>
      {about}
      {why && <div className="why">{why}</div>}
      {confirm}
      <div className="peek">
        {tools.map((tool) => (
          // 一行一个工具：它叫什么、它自己说它干什么、以及这一刀下去会不会动
          // 你的东西。schema 被拒的那些照列，但写明为什么调不了。
          <div className="trow" key={tool.name} data-bad={tool.error ? "" : undefined}>
            <span className="nm">{tool.name}</span>
            <span className="ds">{tool.error || tool.description || t("没有写说明")}</span>
            <span className="face">
              {tool.destructive ? <i className="dg">{t("会改东西")}</i> : tool.readOnly ? <i className="ro">{t("只读")}</i> : null}
            </span>
          </div>
        ))}
      </div>
    </details>
  );
}

// How a skill can start is the thing the slash list cannot say: a slash name
// means you can call it, "auto" means the model may start it on its own, and
// those are separate permissions. Both is the norm, so only the row that is
// missing one says anything — a badge on every row is a badge on none.
