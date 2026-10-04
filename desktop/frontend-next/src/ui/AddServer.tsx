import { useId, useState } from "react";
import { useEscape } from "./dismiss";
import { t } from "../i18n";
import type { AgentPort, McpDraftServer, McpInstallResult, McpInstallScope, McpRisk } from "../port/port";
import { reason } from "../i18n/kernel";

// Nobody types a transport into a form: they arrive holding whatever the server's
// docs printed. So the input is one box that takes all three shapes, and the
// kernel decides which one it got.
const PLACEHOLDER = `{"mcpServers": {"github": {"command": "npx", "args": ["-y", "@modelcontextprotocol/server-github"]}}}

npx -y chrome-devtools-mcp@latest

https://mcp.example.com/sse`;

const KIND_LABEL: Record<string, string> = {
  shell: "将在本机运行",
  "unknown-host": "将连接至",
  secret: "密钥",
};

interface Props {
  port: AgentPort;
  canProject: boolean;
  onClose: () => void;
  onInstalled: () => void;
}

export function AddServer(props: Props) {
  const [connection, setConnection] = useState({ port: props.port, generation: 0 });
  if (connection.port !== props.port) setConnection({ port: props.port, generation: connection.generation + 1 });
  return <ServerInput key={connection.generation} {...props} />;
}

function ServerInput({ port, canProject, onClose, onInstalled }: Props) {
  const sourceLabel = useId();
  // Mounted only while open, so this component is the layer Escape closes.
  useEscape(true, onClose);
  const [text, setText] = useState("");
  const [draft, setDraft] = useState<{ servers: McpDraftServer[]; risks: McpRisk[] } | null>(null);
  const [scope, setScope] = useState<McpInstallScope>("user");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [results, setResults] = useState<McpInstallResult[]>([]);
  const remaining = draft?.servers.filter((s) => !results.some((r) => r.name === s.name && r.state !== "issue")) ?? [];
  const hasSaved = results.some((r) => r.state !== "issue");

  const parse = async () => {
    if (busy || !text.trim()) return;
    setBusy(true);
    setError("");
    try {
      setDraft(await port.parseMcp(text));
    } catch (e) {
      setDraft(null);
      setError(reason(e));
    } finally {
      setBusy(false);
    }
  };

  const install = async () => {
    if (!draft) return;
    setBusy(true);
    setError("");
    const out: McpInstallResult[] = [];
    try {
      for (const s of remaining) out.push(await port.installMcp(s, scope));
    } catch (e) {
      setError(reason(e));
    } finally {
      setResults((previous) => [...new Map([...previous, ...out].map((r) => [r.name, r])).values()]);
      setBusy(false);
      if (out.some((r) => r.state !== "issue")) onInstalled();
    }
  };

  if (results.length > 0 && remaining.length === 0 && !error && !busy) {
    return (
      <div className="addsrv" data-stage="done">
        {results.map((r) => (
          <Outcome key={r.name} r={r} />
        ))}
        <div className="acts">
          <button className="act" onClick={onClose}>
            {t("完成")}
          </button>
        </div>
      </div>
    );
  }

  return (
    <div className="addsrv" data-stage={draft ? "confirm" : "paste"} aria-busy={busy}>
      {!draft && (
        <>
          <textarea
            className="paste"
            aria-labelledby={sourceLabel}
            data-action-keydown="mcp.inspect"
            rows={4}
            autoFocus
            value={text}
            disabled={busy}
            placeholder={PLACEHOLDER}
            onChange={(e) => setText(e.target.value)}
            onKeyDown={(e) => {
              if ((e.metaKey || e.ctrlKey) && e.key === "Enter") {
                e.preventDefault();
                void parse();
              }
            }}
          />
          <div className="acts">
            <span className="note" id={sourceLabel}>{t("一段 JSON、一行命令，或一个 https 地址")}</span>
            <button className="act" onClick={onClose}>
              {t("取消")}
            </button>
            <button className="act" data-action="mcp.inspect" data-primary disabled={!text.trim() || busy} onClick={() => void parse()}>
              {t(busy ? "读取中…" : "查看内容")}
            </button>
          </div>
        </>
      )}

      {draft && (
        <>
          {results.map((r) => <Outcome key={r.name} r={r} />)}
          {remaining.map((s) => (
            <div className="cand" key={s.name}>
              <div className="cand-hd">
                <span className="nm">{s.name}</span>
                <span className="meta">{s.transport}</span>
              </div>
              {draft.risks
                .filter((k) => k.server === s.name)
                .map((k) => (
                  <div className="risk" key={k.field} data-kind={k.kind}>
                    <span className="lb">{t(KIND_LABEL[k.kind] ?? k.kind)}</span>
                    <span className="dt">{k.kind === "secret" ? k.field.split(".").pop() : k.detail}</span>
                    {k.kind === "secret" && <span className="why">{k.detail}</span>}
                  </div>
                ))}
            </div>
          ))}
          {/* Installing is a global act; how far it reaches is a separate
              question. Only the third option edits a tracked file, so it is the
              one that has to say so out loud — and it is never the default. */}
          <div className="scope" role="radiogroup" aria-label={t("安装位置")} data-action-keydown="mcp.scope"
            onKeyDown={(e) => {
              if (busy || hasSaved || e.altKey || e.ctrlKey || e.metaKey) return;
              const radios = [...e.currentTarget.querySelectorAll<HTMLButtonElement>('[role="radio"]')].filter((radio) => !radio.disabled);
              const index = radios.indexOf(document.activeElement as HTMLButtonElement);
              if (index < 0) return;
              const next = e.key === "ArrowRight" || e.key === "ArrowDown" ? (index + 1) % radios.length
                : e.key === "ArrowLeft" || e.key === "ArrowUp" ? (index + radios.length - 1) % radios.length
                : e.key === "Home" ? 0 : e.key === "End" ? radios.length - 1 : undefined;
              if (next === undefined) return;
              e.preventDefault();
              e.stopPropagation();
              radios[next].focus();
              radios[next].click();
            }}>
            <button role="radio" aria-checked={scope === "user"} tabIndex={scope === "user" ? 0 : -1}
              data-action="mcp.scope" disabled={busy || hasSaved} onClick={() => setScope("user")}>
              {t("我的")}<i>{t("所有项目均可使用")}</i>
            </button>
            <button
              role="radio"
              aria-checked={scope === "local"}
              tabIndex={scope === "local" ? 0 : -1}
              data-action="mcp.scope"
              disabled={busy || hasSaved || !canProject}
              onClick={() => setScope("local")}
            >
              {t("仅当前项目")}<i>{t("不写入仓库，他人不会获得")}</i>
            </button>
            <button
              role="radio"
              aria-checked={scope === "project"}
              tabIndex={scope === "project" ? 0 : -1}
              data-action="mcp.scope"
              disabled={busy || hasSaved || !canProject}
              onClick={() => setScope("project")}
            >
              {t("写进仓库")}<i>{t("clone 仓库的人也会获得")}</i>
            </button>
          </div>
          {scope === "project" && (
            <div className="warn">{t("这会修改仓库中的配置文件，属于一处待提交的改动。")}</div>
          )}
          <div className="acts">
            <button className="act" disabled={busy} onClick={() => { setDraft(null); setResults([]); setError(""); }}>
              {t("返回")}
            </button>
            <button className="act" data-action="mcp.add" data-primary disabled={busy} onClick={() => void install()}>
              {t(busy ? "连接中…" : "接入")}
            </button>
          </div>
        </>
      )}

      {error && <div className="why">{error}</div>}
    </div>
  );
}

// "Saved" and "usable" are different outcomes, and the user is waiting to hear
// which one happened — so each state says what is true and what to do next.
function Outcome({ r }: { r: McpInstallResult }) {
  const done = r.state === "ready";
  const auth = r.state === "action_required";
  return (
    <div className="outcome" data-state={r.state}>
      <i className="pip" />
      <span className="nm">{r.name}</span>
      <span className="dt">
        {done && t("已就位 · {n} 个工具，下一轮就能用", { n: r.toolCount })}
        {auth && t("配置留下了，去授权后在列表里点重连")}
        {!done && !auth && (r.message || t("没装上，什么都没留下"))}
      </span>
    </div>
  );
}
