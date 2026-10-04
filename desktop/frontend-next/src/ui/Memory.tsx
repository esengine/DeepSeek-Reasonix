import { useEffect, useRef, useState } from "react";
import { t } from "../i18n";
import { tx } from "../i18n/rich";
import { reason } from "../i18n/kernel";
import { Group } from "./Group";
import { Switch } from "./Switch";
import type { AgentPort, MemoryEdit, MemoryEntry, RememberApprovalSettings } from "../port/port";

// Memory is the only thing here that changes how the agent behaves without the
// user ever configuring it — the agent writes it. So the grouping answers the
// question that actually gets asked: when does this one apply?
const GROUPS: [string, string, string][] = [
  ["pinned", "始终生效", "每轮均写入提示词，等同于长期指令"],
  ["relevant", "相关时才会检索", "仅在当前轮次相关时才会被检索"],
];

const SCOPE: Record<string, string> = { project: "项目", global: "我的" };

export function Memory({ port }: { port: AgentPort }) {
  const [connection, setConnection] = useState({ port, generation: 0 });
  if (connection.port !== port) setConnection({ port, generation: connection.generation + 1 });
  return <MemoryPanel key={connection.generation} port={port} />;
}

function MemoryPanel({ port }: { port: AgentPort }) {
  const [items, setItems] = useState<MemoryEntry[] | null>(null);
  const [unread, setUnread] = useState("");
  const [query, setQuery] = useState("");
  const [open, setOpen] = useState("");
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");
  const [edit, setEdit] = useState<MemoryEdit | null>(null);
  const [past, setPast] = useState<Record<string, MemoryEntry[]>>({});
  const [showPast, setShowPast] = useState("");
  const historyRequests = useRef<Record<string, object>>({});

  const reload = () => {
    port
      .memories()
      .then((c) => {
        setItems(c.memories);
        setQuery(c.recallQuery);
        setUnread("");
      })
      .catch((e) => {
        setItems(null);
        setUnread(reason(e));
      });
  };
  useEffect(reload, [port]); // eslint-disable-line react-hooks/exhaustive-deps

  // This panel reads, rewrites and removes facts; writing a new one is the
  // composer's /remember — the same command the CLI's memory view points at.
  // Naming it here rather than opening a second write path keeps one way in,
  // and this page had none at all: an empty store was a dead end.
  const authoring = (
    <p className="note">{tx("如需手动记录，请在输入框中使用 {cmd} 命令。", { cmd: <code>/remember</code> })}</p>
  );

  // Three answers, not two: a request still out is not a store that refused,
  // and reading the first as the second told every reader their memory was
  // broken for as long as the first fetch took.
  if (unread)
    return (
      <div className="find" data-lvl="warn" role="alert">
        <span className="t">{t("无法读取记忆")}</span>
        <span className="why">
          {unread}
          <button className="lnk" data-action="memory.reload" onClick={reload}>
            {t("重试")}
          </button>
        </span>
      </div>
    );
  if (!items) return <div className="empty">{t("正在读取记忆…")}</div>;
  if (items.length === 0)
    return (
      <>
        <div className="empty">{t("暂无记录。")}</div>
        {authoring}
      </>
    );

  const refreshMemory = (name: string) => {
    delete historyRequests.current[name];
    setPast((p) => {
      const next = { ...p };
      delete next[name];
      return next;
    });
    setShowPast((shown) => shown === name ? "" : shown);
    reload();
  };

  const save = async () => {
    if (!edit) return;
    setBusy(edit.name);
    setError("");
    try {
      await port.saveMemory(edit);
      setEdit(null);
      refreshMemory(edit.name);
    } catch (e) {
      setError(reason(e));
    } finally {
      setBusy("");
    }
  };

  const openHistory = async (name: string) => {
    if (showPast === name) return setShowPast("");
    setShowPast(name);
    setError("");
    if (past[name]) return;
    const request = {};
    historyRequests.current[name] = request;
    try {
      const list = await port.memoryRevisions(name);
      if (historyRequests.current[name] !== request) return;
      setPast((p) => ({ ...p, [name]: list }));
    } catch (e) {
      if (historyRequests.current[name] !== request) return;
      setError(reason(e));
      setShowPast((shown) => shown === name ? "" : shown);
    }
  };

  const restore = async (name: string, revision: number) => {
    setBusy(name);
    setError("");
    try {
      await port.restoreMemory(name, revision);
      refreshMemory(name);
    } catch (e) {
      setError(reason(e));
    } finally {
      setBusy("");
    }
  };

  const forget = async (name: string) => {
    setBusy(name);
    setError("");
    try {
      await port.forgetMemory(name);
      refreshMemory(name);
    } catch (e) {
      setError(reason(e));
    } finally {
      setBusy("");
    }
  };

  const usedCount = items.filter((m) => m.usedLastTurn).length;

  return (
    <div className="mem">
      {query && (
        <p className="recall">
          {t("上一轮以「{q}」为线索检索了一次记忆", { q: clip(query) })}
          {usedCount > 0 ? t("，命中 {n} 条", { n: usedCount }) : t("，未命中任何条目")}
        </p>
      )}
      {GROUPS.map(([id, label, desc]) => {
        const group = items.filter((m) => m.activation === id);
        if (group.length === 0) return null;
        return (
          <section className="memgrp" key={id}>
            <div className="hd">
              <span className="lb">{t(label)}</span>
              <span className="c">{group.length}</span>
            </div>
            <p className="ds">{t(desc)}</p>
            {group.map((m) => (
              <div className="memrow" key={m.name} data-used={m.usedLastTurn ? "" : undefined}>
                <div className="line">
                  <i className="dot" title={m.usedLastTurn ? t("上一轮已使用") : undefined} />
                  <button className="nm" onClick={() => setOpen(open === m.name ? "" : m.name)}>
                    {m.title || m.name}
                  </button>
                  <span className="ds" title={m.description}>{m.description}</span>
                  {m.expired && <i className="stale">{t("已过期")}</i>}
                  <span className="sc">{t(SCOPE[m.scope ?? ""] ?? m.scope ?? "")}</span>
                  <span className="at">{m.updatedAt || m.createdAt}</span>
                  <button className="act ghost" data-action="memory.forget" data-target={m.name} disabled={busy === m.name} onClick={() => void forget(m.name)}>
                    {t(busy === m.name ? "…" : "忘记")}
                  </button>
                </div>
                {m.usedLastTurn && m.why && <div className="why-used">{t("上一轮因「{why}」被检索到", { why: m.why })}</div>}
                {open === m.name && (
                  <div className="peek">
                    {edit?.name === m.name ? (
                      <div className="memedit">
                        <label>
                          {t("标题")}
                          <input value={edit.title} onChange={(e) => setEdit({ ...edit, title: e.target.value })} />
                        </label>
                        <label>
                          {t("简要说明")}
                          <input value={edit.description} onChange={(e) => setEdit({ ...edit, description: e.target.value })} />
                        </label>
                        <label>
                          {t("正文")}
                          <textarea rows={8} value={edit.body} onChange={(e) => setEdit({ ...edit, body: e.target.value })} />
                        </label>
                        <label className="when">
                          {t("生效时机")}
                          <select value={edit.activation} onChange={(e) => setEdit({ ...edit, activation: e.target.value })}>
                            <option value="relevant">{t("相关时启用")}</option>
                            <option value="pinned">{t("每轮常驻")}</option>
                          </select>
                        </label>
                        <div className="row">
                          <button className="act" data-action="memory.save" data-target={m.name} disabled={busy === m.name} onClick={() => void save()}>
                            {t(busy === m.name ? "正在保存…" : "保存")}
                          </button>
                          <button className="act ghost" onClick={() => setEdit(null)}>{t("取消")}</button>
                          {/* Saving writes a new revision rather than overwriting, which is
                              what makes offering an edit at all safe. */}
                          <span className="hint">{t("保存将记为新版本，旧版本仍保留")}</span>
                        </div>
                      </div>
                    ) : (
                      <>
                        <pre>{m.body?.trim() || t("（无正文）")}</pre>
                        <div className="row">
                          <button
                            className="act ghost"
                            onClick={() => setEdit({ name: m.name, title: m.title ?? "", description: m.description ?? "",
                              body: m.body ?? "", activation: m.activation })}
                          >
                            {t("编辑")}
                          </button>
                          {(m.revision ?? 1) > 1 && (
                            <button className="act ghost" onClick={() => void openHistory(m.name)}>
                              {t(showPast === m.name ? "收起旧版本" : "第 {n} 版，查看历史", { n: m.revision ?? 1 })}
                            </button>
                          )}
                          {m.path && <span className="path">{m.path}</span>}
                        </div>
                        {showPast === m.name && <History
                          list={past[m.name]}
                          current={m.revision ?? 1}
                          busy={busy === m.name}
                          onRestore={(rev) => void restore(m.name, rev)}
                        />}
                      </>
                    )}
                  </div>
                )}
              </div>
            ))}
          </section>
        );
      })}
      {error && <div className="why">{error}</div>}
      {authoring}
    </div>
  );
}

// The panel already promised that saving keeps the old version. This is where
// that promise becomes reachable: the revisions behind the current one, and a
// way back into any of them. Restoring appends a revision rather than rewinding
// to one, so nothing a reader is looking at disappears when they use it.
function History({ list, current, busy, onRestore }: {
  list: MemoryEntry[] | undefined;
  current: number;
  busy: boolean;
  onRestore: (revision: number) => void;
}) {
  if (!list) return <div className="memhist"><span className="ds">{t("正在读取历史版本…")}</span></div>;
  const older = list.filter((m) => (m.revision ?? 1) !== current);
  if (older.length === 0) return <div className="memhist"><span className="ds">{t("仅有当前版本。")}</span></div>;
  return (
    <div className="memhist">
      {older.map((m) => (
        <div className="histrow" key={m.revision}>
          <span className="rev">{t("第 {n} 版", { n: m.revision ?? 1 })}</span>
          <span className="at">{m.updatedAt || m.createdAt}</span>
          <span className="ds" title={m.title}>{m.title}</span>
          <button className="act ghost" data-action="memory.restore" data-target={m.name} disabled={busy} onClick={() => onRestore(m.revision ?? 1)}>
            {t(busy ? "…" : "恢复此版本")}
          </button>
          <pre>{clip2(m.body)}</pre>
        </div>
      ))}
      <span className="hint">{t("恢复同样记为新版本，历史版本均保留")}</span>
    </div>
  );
}

function clip2(s: string | undefined): string {
  const body = (s ?? "").trim();
  if (!body) return "（无正文）";
  return body.length > 200 ? body.slice(0, 200) + "…" : body;
}

function clip(s: string): string {
  const t = s.trim();
  return t.length > 24 ? t.slice(0, 24) + "…" : t;
}

// One answer: a project write may stop asking when the user says so, while a
// global fact keeps asking — it reaches every project. forget keeps asking
// either way.
export function RememberApproval({ port, onChanged }: { port: AgentPort; onChanged: () => void }) {
  const [state, setState] = useState<RememberApprovalSettings | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    port
      .rememberApproval()
      .then(setState)
      .catch(() => setState(null));
  }, [port]);

  if (!state) return <div className="empty">{t("无法读取记忆写入设置。")}</div>;

  const flip = async () => {
    setBusy(true);
    setError("");
    try {
      setState(await port.saveRememberApproval({ projectAutoConfirm: !state.projectAutoConfirm }));
      onChanged();
    } catch (e) {
      setError(reason(e));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="box">
      <div className="lrow">
        <span className="tx">
          <span className="lb">{t("记忆写入免确认")}</span>
          <span className="ds">{t("开启后，agent 保存项目记忆时不再逐次询问。全局记忆会进入每个项目，因此仍然每次询问；忘记（forget）也一样。修改会重建运行时，任务运行期间无法变更。")}</span>
        </span>
      </div>
      <div className="lrow">
        <span className="tx">
          <span className="lb">{t("项目记忆")}</span>
        </span>
        <Switch
          data-action="remember-approval.project"
          on={state.projectAutoConfirm}
          busy={busy}
          label={t("项目记忆写入免确认")}
          onClick={() => void flip()}
        />
      </div>
      {error && <div className="why">{error}</div>}
    </div>
  );
}

// The block ships whole so the settings page keeps one line for it: this file
// already owns the memory panel, and the page that held the frame sits at its
// line ceiling.
export function MemoryGroup({ port, onChanged }: { port: AgentPort; onChanged: () => void }) {
  return (
    <Group
      id="memory"
      title={t("记忆")}
      hint={t("agent 自动记录的内容：未经配置，但会据此执行。此处按触发时机分组，并标出上一轮实际使用的条目。")}
    >
      <RememberApproval port={port} onChanged={onChanged} />
      <Memory port={port} />
    </Group>
  );
}
