import { useEffect, useState } from "react";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import type { AgentPort, MemoryEdit, MemoryEntry } from "../port/port";

// Memory is the only thing here that changes how the agent behaves without the
// user ever configuring it — the agent writes it. So the grouping answers the
// question that actually gets asked: when does this one apply?
const GROUPS: [string, string, string][] = [
  ["pinned", "一直生效", "每一轮都在提示词里，等同于你给它的长期指令"],
  ["relevant", "相关时才被想起", "只有这一轮看起来相关时才会被翻出来"],
];

const SCOPE: Record<string, string> = { project: "项目", global: "我的" };

export function Memory({ port }: { port: AgentPort }) {
  const [items, setItems] = useState<MemoryEntry[] | null>(null);
  const [query, setQuery] = useState("");
  const [open, setOpen] = useState("");
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");
  const [edit, setEdit] = useState<MemoryEdit | null>(null);
  const [past, setPast] = useState<Record<string, MemoryEntry[]>>({});
  const [showPast, setShowPast] = useState("");

  const reload = () => {
    port
      .memories()
      .then((c) => {
        setItems(c.memories);
        setQuery(c.recallQuery);
      })
      .catch(() => setItems(null));
  };
  useEffect(reload, [port]); // eslint-disable-line react-hooks/exhaustive-deps

  if (!items) return <div className="empty">{t("读不到记忆。")}</div>;
  if (items.length === 0) return <div className="empty">{t("还没有记下任何东西。")}</div>;

  const save = async () => {
    if (!edit) return;
    setBusy(edit.name);
    setError("");
    try {
      await port.saveMemory(edit);
      setEdit(null);
      reload();
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
    try {
      const list = await port.memoryRevisions(name);
      setPast((p) => ({ ...p, [name]: list }));
    } catch (e) {
      setError(reason(e));
      setShowPast("");
    }
  };

  const restore = async (name: string, revision: number) => {
    setBusy(name);
    setError("");
    try {
      await port.restoreMemory(name, revision);
      // The restore wrote a new revision, so the cached list is now one short.
      // Drop the key rather than emptying it — an empty array reads as cached.
      setPast((p) => {
        const next = { ...p };
        delete next[name];
        return next;
      });
      setShowPast("");
      reload();
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
      reload();
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
          {t("上一轮从「{q}」出发翻了一次记忆", { q: clip(query) })}
          {usedCount > 0 ? t("，用上了 {n} 条", { n: usedCount }) : t("，一条都没用上")}
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
                  <i className="dot" title={m.usedLastTurn ? t("上一轮用上了") : undefined} />
                  <button className="nm" onClick={() => setOpen(open === m.name ? "" : m.name)}>
                    {m.title || m.name}
                  </button>
                  <span className="ds" title={m.description}>{m.description}</span>
                  {m.expired && <i className="stale">{t("已过期")}</i>}
                  <span className="sc">{t(SCOPE[m.scope ?? ""] ?? m.scope ?? "")}</span>
                  <span className="at">{m.updatedAt || m.createdAt}</span>
                  <button className="act ghost" disabled={busy === m.name} onClick={() => void forget(m.name)}>
                    {t(busy === m.name ? "…" : "忘掉")}
                  </button>
                </div>
                {m.usedLastTurn && m.why && <div className="why-used">{t("上一轮因为「{why}」被翻出来", { why: m.why })}</div>}
                {open === m.name && (
                  <div className="peek">
                    {edit?.name === m.name ? (
                      <div className="memedit">
                        <label>
                          {t("标题")}
                          <input value={edit.title} onChange={(e) => setEdit({ ...edit, title: e.target.value })} />
                        </label>
                        <label>
                          {t("一句话说明")}
                          <input value={edit.description} onChange={(e) => setEdit({ ...edit, description: e.target.value })} />
                        </label>
                        <label>
                          {t("正文")}
                          <textarea rows={8} value={edit.body} onChange={(e) => setEdit({ ...edit, body: e.target.value })} />
                        </label>
                        <label className="when">
                          {t("什么时候用上")}
                          <select value={edit.activation} onChange={(e) => setEdit({ ...edit, activation: e.target.value })}>
                            <option value="relevant">{t("相关时想起")}</option>
                            <option value="pinned">{t("每轮都在")}</option>
                          </select>
                        </label>
                        <div className="row">
                          <button className="act" disabled={busy === m.name} onClick={() => void save()}>
                            {t(busy === m.name ? "正在保存…" : "保存")}
                          </button>
                          <button className="act ghost" onClick={() => setEdit(null)}>{t("取消")}</button>
                          {/* Saving writes a new revision rather than overwriting, which is
                              what makes offering an edit at all safe. */}
                          <span className="hint">{t("保存会记成新的一版，旧的还在")}</span>
                        </div>
                      </div>
                    ) : (
                      <>
                        <pre>{m.body?.trim() || t("（没有正文）")}</pre>
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
                              {t(showPast === m.name ? "收起旧版本" : "第 {n} 版，看旧的", { n: m.revision ?? 1 })}
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
  if (!list) return <div className="memhist"><span className="ds">{t("正在读旧版本…")}</span></div>;
  const older = list.filter((m) => (m.revision ?? 1) !== current);
  if (older.length === 0) return <div className="memhist"><span className="ds">{t("只有当前这一版。")}</span></div>;
  return (
    <div className="memhist">
      {older.map((m) => (
        <div className="histrow" key={m.revision}>
          <span className="rev">{t("第 {n} 版", { n: m.revision ?? 1 })}</span>
          <span className="at">{m.updatedAt || m.createdAt}</span>
          <span className="ds" title={m.title}>{m.title}</span>
          <button className="act ghost" disabled={busy} onClick={() => onRestore(m.revision ?? 1)}>
            {t(busy ? "…" : "恢复这版")}
          </button>
          <pre>{clip2(m.body)}</pre>
        </div>
      ))}
      <span className="hint">{t("恢复也会记成新的一版，这些都还在")}</span>
    </div>
  );
}

function clip2(s: string | undefined): string {
  const body = (s ?? "").trim();
  if (!body) return "（没有正文）";
  return body.length > 200 ? body.slice(0, 200) + "…" : body;
}

function clip(s: string): string {
  const t = s.trim();
  return t.length > 24 ? t.slice(0, 24) + "…" : t;
}
