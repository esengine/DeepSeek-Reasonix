import { useState } from "react";
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
  shell: "会在你机器上运行",
  "unknown-host": "会连到",
  secret: "密钥",
};

interface Props {
  port: AgentPort;
  canProject: boolean;
  onClose: () => void;
  onInstalled: () => void;
}

export function AddServer({ port, canProject, onClose, onInstalled }: Props) {
  const [text, setText] = useState("");
  const [draft, setDraft] = useState<{ servers: McpDraftServer[]; risks: McpRisk[] } | null>(null);
  const [scope, setScope] = useState<McpInstallScope>("user");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [results, setResults] = useState<McpInstallResult[]>([]);

  const parse = async () => {
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
    try {
      const out: McpInstallResult[] = [];
      for (const s of draft.servers) out.push(await port.installMcp(s, scope));
      setResults(out);
      if (out.some((r) => r.state !== "issue")) onInstalled();
    } catch (e) {
      setError(reason(e));
    } finally {
      setBusy(false);
    }
  };

  if (results.length > 0) {
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
    <div className="addsrv" data-stage={draft ? "confirm" : "paste"}>
      {!draft && (
        <>
          <textarea
            className="paste"
            rows={4}
            autoFocus
            value={text}
            placeholder={PLACEHOLDER}
            onChange={(e) => setText(e.target.value)}
            onKeyDown={(e) => {
              if ((e.metaKey || e.ctrlKey) && e.key === "Enter") void parse();
            }}
          />
          <div className="acts">
            <span className="note">{t("一段 JSON、一行命令，或者一个 https 地址")}</span>
            <button className="act" onClick={onClose}>
              {t("取消")}
            </button>
            <button className="act" data-primary disabled={!text.trim() || busy} onClick={() => void parse()}>
              {t(busy ? "读取中…" : "看看是什么")}
            </button>
          </div>
        </>
      )}

      {draft && (
        <>
          {draft.servers.map((s) => (
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
          <div className="scope" role="radiogroup" aria-label={t("装到哪")}>
            <button role="radio" aria-checked={scope === "user"} onClick={() => setScope("user")}>
              {t("我的")}<i>{t("所有项目里都能用")}</i>
            </button>
            <button
              role="radio"
              aria-checked={scope === "local"}
              disabled={!canProject}
              onClick={() => setScope("local")}
            >
              {t("只在这个项目")}<i>{t("不写进仓库，别人不会拿到")}</i>
            </button>
            <button
              role="radio"
              aria-checked={scope === "project"}
              disabled={!canProject}
              onClick={() => setScope("project")}
            >
              {t("写进仓库")}<i>{t("clone 的人也会拿到")}</i>
            </button>
          </div>
          {scope === "project" && (
            <div className="warn">{t("这会改动仓库里的配置文件，是一处等着提交的改动。")}</div>
          )}
          <div className="acts">
            <button className="act" onClick={() => setDraft(null)}>
              {t("返回")}
            </button>
            <button className="act" data-primary disabled={busy} onClick={() => void install()}>
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
        {done && `已就位 · ${r.toolCount} 个工具，下一轮就能用`}
        {auth && "配置留下了，去授权后在列表里点重连"}
        {!done && !auth && (r.message || "没装上，什么都没留下")}
      </span>
    </div>
  );
}
