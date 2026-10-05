import { useRef, useState } from "react";
import { t } from "../i18n";
import type { AgentPort, McpEntry } from "../port/port";
import { Exception } from "./CapabilityScope";
import { Clip } from "./Clip";
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
  failed: "无法连接",
  disabled: "已关闭",
  standby: "待命 · 首次调用时启动",
  idle: "未连接",
  // Project declarations require approval before the server can launch.
  pending: "待授权 · 来自项目声明",
};

// A tag only when the schema carries the server's tools or config asks it to:
// deferred is the default, and a badge on every row is a badge on none.
function loadTag(m: McpEntry): { text: string; pending: boolean } | null {
  if (m.alwaysLoad) return { text: m.inSchema ? t("常驻") : t("常驻 · 尚未生效"), pending: !m.inSchema };
  if (m.inSchema) return { text: t("常驻 · 待撤下"), pending: true };
  return null;
}

// The tool list is the long half and the least urgent, so it folds behind its
// own count — the same idiom a long file read uses in the transcript.
export function ServerRow({
  m, port, onDone, root, live,
}: {
  m: McpEntry; port: AgentPort; onDone: () => void; root: string; live: boolean;
}) {
  const [owner, setOwner] = useState({ port, root });
  const currentOwner = useRef(owner);
  currentOwner.current = owner;
  const [busy, setBusy] = useState("");
  const [failed, setFailed] = useState("");
  const [confirming, setConfirming] = useState(false);
  if (owner.port !== port || owner.root !== root) {
    setOwner({ port, root });
    setBusy("");
    setFailed("");
    setConfirming(false);
  }
  // 状态码是主机答上来的事实，画出来即可；它是不是「需要你重新认证」还要看自动
  // 刷新有没有跑过，那件事目前无人知道。m.error 是外部服务器自己写的文本，只作
  // 详情显示,不参与任何判断。
  const meta = [t(MCP_STATE[m.state] ?? m.state), m.transport, m.source, m.httpStatus ? `HTTP ${m.httpStatus}` : ""]
    .filter(Boolean)
    .join(" · ");

  const run = async (what: string, fn: () => Promise<unknown>) => {
    setBusy(what);
    setFailed("");
    try {
      const r = (await fn()) as { error?: string } | void;
      if (currentOwner.current === owner && r && typeof r === "object" && r.error) setFailed(r.error);
    } catch (e) {
      if (currentOwner.current === owner) setFailed(reason(e));
    } finally {
      if (currentOwner.current === owner) setBusy("");
      onDone();
    }
  };

  const actions = (
    <span className="acts">
      {live && m.enabled && m.state !== "ready" && m.state !== "pending" && (
        <button className="act" data-action="mcp.retry" data-target={m.name} disabled={!!busy} onClick={() => void run("retry", () => port.reconnectMcp(m.name))}>
          {t(busy === "retry" ? "连接中…" : m.state === "standby" ? "立即连接" : "重连")}
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
        busy={!!busy}
        label={t(m.enabled ? "关闭 {name}" : "启用 {name}", { name: m.name })}
        onClick={() => void run("toggle", () => port.setMcpEnabled(m.name, !m.enabled, "project", root || undefined))}
      />
    </span>
  );

  const confirm = confirming && (
    <div className="confirm">
      <span className="q">
        {t("从 {where} 中删除 {name}？若只是暂时停用，关闭开关即可。", { name: m.name, where: m.source || t("配置") })}
      </span>
      <button className="act" data-action="mcp.remove" data-target={m.name} data-value="cancel" disabled={!!busy} onClick={() => setConfirming(false)}>
        {t("取消")}
      </button>
      <button
        className="act danger"
        data-action="mcp.remove"
        data-target={m.name}
        data-value="confirm"
        disabled={!!busy}
        onClick={() =>
          void run("remove", async () => {
            const r = await port.removeMcp(m.name);
            if (currentOwner.current !== owner) return;
            setConfirming(false);
            // A lower-precedence declaration with the same name may have taken
            // over; saying so beats a list that looks like the delete failed.
            if (r.stillConfigured) setFailed(t("同名的另一处声明已生效，该行不会消失。"));
          })
        }
      >
        {t(busy === "remove" ? "移除中…" : "移除")}
      </button>
    </div>
  );

  const tools = m.toolList ?? [];
  const tag = loadTag(m);
  // The tool list is what gets loaded, so the choice sits above it; a server
  // whose tools are not known yet has nothing to load either way.
  const load = live && m.enabled && tools.length > 0 && (
    <div className="load">
      <span className="lb">{t("加载方式")}</span>
      <div className="seg" data-text role="group" aria-label={t("加载方式")}>
        {(["deferred", "always"] as const).map((mode) => (
          <button key={mode} data-action="mcp.load" data-target={m.name} data-value={mode}
            aria-pressed={(mode === "always") === !!m.alwaysLoad} disabled={!!busy}
            onClick={() => {
              if ((mode === "always") !== !!m.alwaysLoad) void run("load", () => port.setMcpLoad(m.name, mode));
            }}>
            {t(mode === "always" ? "常驻" : "按需")}
          </button>
        ))}
      </div>
      <span className="hn">
        {t(m.alwaysLoad
          ? "启动时预先连接，工具每轮随请求发送，模型直接调用，省去搜索往返，但占用上下文。"
          : "用到时再搜索加载，不占上下文，调用前多一轮往返。")}
      </span>
    </div>
  );
  const head = (
    <>
      <i className="pip" />
      <Clip className="nm">{m.name}</Clip>
      {tools.length ? <Clip className="fold">{t("{n} 个工具", { n: m.tools })}</Clip> : null}
      {tag && <i className="ld" data-pending={tag.pending ? "" : undefined}>{tag.text}</i>}
      <Clip className="meta">{meta}</Clip>
      {m.localOverride && <Exception onClear={() => void run("clear", () => port.clearMcpOverride(m.name, root || undefined))} busy={!!busy} />}
      {actions}
    </>
  );
  // 服务是干什么的，只有服务自己说了算：MCP 握手里的那段自述。它没写，这里就
  // 没有 —— 拿名字或配置凑一句出来，等于替它编。
  const about = (!!m.description || (!tools.length && m.state !== "connecting")) && (
    <div className="srv-ab">
      <span className="ds">{m.description || t("该服务未提供自我说明。")}</span>
      {m.remembered && (
        <i className="w" title={t("当前未连接，以下是上次连接时它返回的内容。")}>
          {t(m.stale ? "上次连上时的记录 · 声明改过，可能对不上了" : "上次连接时的记录")}
        </i>
      )}
    </div>
  );
  const why = failed || m.error;
  if (!tools.length) {
    return (
      <div className="srv mcp-server" data-st={m.state} data-local={m.localOverride ? "" : undefined} aria-busy={!!busy}>
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
    <details className="srv mcp-server" data-st={m.state} data-local={m.localOverride ? "" : undefined} aria-busy={!!busy} open={confirming || undefined}>
      <summary>{head}</summary>
      {about}
      {why && <div className="why">{why}</div>}
      {confirm}
      {load}
      <div className="peek">
        {tools.map((tool) => (
          // 一行一个工具：它叫什么、它自己说它干什么、以及这一刀下去会不会动
          // 你的东西。schema 被拒的那些照列，但写明为什么调不了。
          <div className="trow" key={tool.name} data-bad={tool.error ? "" : undefined}>
            <span className="nm">{tool.name}</span>
            <Clip className="ds">{tool.error || tool.description || t("未提供说明")}</Clip>
            <span className="face">
              {tool.destructive ? <i className="dg">{t("会修改数据")}</i> : tool.readOnly ? <i className="ro">{t("只读")}</i> : null}
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
