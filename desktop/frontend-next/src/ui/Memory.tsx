import { useEffect, useMemo, useRef, useState } from "react";
import { t } from "../i18n";
import { tx } from "../i18n/rich";
import { reason } from "../i18n/kernel";
import type { AgentPort, MemoryEdit, MemoryEntry, UnreadableMemory } from "../port/port";
import { History, MemoryDetail, TYPES, TYPE_NAME, typeOf } from "./MemoryDetail";
import { Icon, StateIcon } from "./memicons";

const PAGE = 30;

type Filter = { kind: "all" | "new" | "constraint" | "faded" } | { kind: "type" | "galaxy"; value: string };

const sameFilter = (a: Filter, b: Filter) =>
  a.kind === b.kind && ("value" in a ? a.value : "") === ("value" in b ? b.value : "");

function matches(m: MemoryEntry, f: Filter): boolean {
  switch (f.kind) {
    case "all": return true;
    case "new": return !!m.new;
    case "constraint": return typeOf(m) === "constraint";
    case "faded": return m.state === "faded" && !m.noDecay;
    case "type": return typeOf(m) === f.value;
    case "galaxy": return (m.galaxy ?? "") === f.value;
  }
}

function searched(m: MemoryEntry, q: string): boolean {
  if (!q) return true;
  const hay = [m.title, m.name, m.description, m.body, m.galaxy].join("\n").toLowerCase();
  return q.toLowerCase().split(/\s+/).filter(Boolean).every((w) => hay.includes(w));
}

const recency = (m: MemoryEntry) => m.lastUsedAt || m.updatedAt || m.createdAt || "";

export function Memory({ port }: { port: AgentPort }) {
  const [connection, setConnection] = useState({ port, generation: 0 });
  if (connection.port !== port) setConnection({ port, generation: connection.generation + 1 });
  return <MemoryPanel key={connection.generation} port={port} />;
}

function MemoryPanel({ port }: { port: AgentPort }) {
  const [items, setItems] = useState<MemoryEntry[] | null>(null);
  const [unreadable, setUnreadable] = useState<UnreadableMemory[]>([]);
  const [unread, setUnread] = useState("");
  const [query, setQuery] = useState("");
  const [open, setOpen] = useState("");
  const [filter, setFilter] = useState<Filter>({ kind: "all" });
  const [search, setSearch] = useState("");
  const [shown, setShown] = useState(PAGE);
  const [busy, setBusy] = useState<string[]>([]);
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
        setUnreadable(c.unreadable ?? []);
        setQuery(c.recallQuery);
        setUnread("");
      })
      .catch((e) => {
        setItems(null);
        setUnreadable([]);
        setUnread(reason(e));
      });
  };
  useEffect(reload, [port]); // eslint-disable-line react-hooks/exhaustive-deps

  const galaxies = useMemo(() => {
    const count = new Map<string, number>();
    for (const m of items ?? []) if (m.galaxy) count.set(m.galaxy, (count.get(m.galaxy) ?? 0) + 1);
    return [...count].sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0]));
  }, [items]);

  const authoring = (
    <p className="note">{tx("如需手动记录，请在输入框中使用 {cmd} 命令。", { cmd: <code>/remember</code> })}</p>
  );

  const unreadableNotice = unreadable.length > 0 ? (
    <div className="find" data-lvl="warn" role="alert">
      <span className="t">{t("这些记忆无法读取")}</span>
      <span className="why">{unreadable.map((u) => `${u.name}: ${u.error}`).join("；")}</span>
    </div>
  ) : null;

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
        {unreadableNotice}
        {unreadable.length === 0 && <div className="empty">{t("暂无记录。")}</div>}
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
    setShowPast((s) => s === name ? "" : s);
    reload();
  };

  const run = async (name: string, action: () => Promise<void>, after?: () => void) => {
    setBusy((names) => [...names, name]);
    setError("");
    try {
      await action();
      after?.();
      refreshMemory(name);
    } catch (e) {
      setError(reason(e));
    } finally {
      setBusy((names) => names.filter((target) => target !== name));
    }
  };

  const save = () => {
    if (!edit) return;
    const draft = edit;
    void run(draft.name, () => port.saveMemory(draft), () => setEdit((current) => current === draft ? null : current));
  };
  const confirm = (name: string) => void run(name, () => port.confirmMemory(name));
  const forget = (name: string) => void run(name, () => port.forgetMemory(name));
  const restore = (name: string, revision: number) => void run(name, () => port.restoreMemory(name, revision));

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
      setShowPast((s) => s === name ? "" : s);
    }
  };

  const count = (f: Filter) => items.filter((m) => matches(m, f)).length;
  const list = items
    .filter((m) => matches(m, filter) && searched(m, search.trim()))
    .sort((a, b) => recency(b).localeCompare(recency(a)) || (a.title || a.name).localeCompare(b.title || b.name));
  const page = list.slice(0, shown);
  const selected = list.find((m) => m.name === open) ?? (filter.kind === "new" ? undefined : page[0]);
  const usedCount = items.filter((m) => m.usedLastTurn).length;

  const pick = (f: Filter) => {
    setFilter(f);
    setShown(PAGE);
  };
  const railItem = (f: Filter, icon: React.ReactNode, label: string) => (
    <button key={label + ("value" in f ? f.value : "")} className="memnav" data-action="memory.filter" data-target={"value" in f ? f.value : f.kind} aria-pressed={sameFilter(filter, f)} onClick={() => pick(f)}>
      {icon}
      <span className="lb">{label}</span>
      <span className="c">{count(f)}</span>
    </button>
  );

  return (
    <div className="mem" data-view={filter.kind}>
      {unreadableNotice}
      {query && (
        <p className="recall">
          {t("上一轮以「{q}」为线索检索了一次记忆", { q: clip(query) })}
          {usedCount > 0 ? t("，命中 {n} 条", { n: usedCount }) : t("，未命中任何条目")}
        </p>
      )}
      <div className="memgrid">
        <nav className="memrail" aria-label={t("记忆分类")}>
          {railItem({ kind: "all" }, <Icon name="all" />, t("全部"))}
          {railItem({ kind: "new" }, <Icon name="new" />, t("新的"))}
          {railItem({ kind: "constraint" }, <Icon name="constraint" />, t("约束"))}
          <div className="sec">{t("类型")}</div>
          {TYPES.filter(([id]) => id !== "constraint").map(([id, name]) => railItem({ kind: "type", value: id }, <Icon name={id} />, t(name)))}
          {galaxies.length > 0 && <div className="sec">{t("星系")}</div>}
          {galaxies.map(([g]) => railItem({ kind: "galaxy", value: g }, <Icon name="galaxy" />, g))}
          <div className="sec" />
          {railItem({ kind: "faded" }, <StateIcon state="faded" />, t("已淡忘"))}
        </nav>
        <div className="memlist">
          <label className="memsearch">
            <Icon name="search" size={14} />
            <input type="search" data-action="memory.search" placeholder={t("搜索记忆")} aria-label={t("搜索记忆")} value={search}
              onChange={(e) => { setSearch(e.target.value); setShown(PAGE); }} />
          </label>
          {page.length === 0 && <div className="empty">{t(filter.kind === "new" ? "没有等你确认的记忆。" : "没有符合的记忆。")}</div>}
          {page.map((m) => (
            <div className="memrow" key={m.name} data-target={m.name} data-used={m.usedLastTurn ? "" : undefined}
              aria-current={selected?.name === m.name || undefined} data-dim={m.state === "faded" && !m.noDecay ? "" : undefined}>
              <button className="nm" data-action="memory.open" data-target={m.name} onClick={() => setOpen(m.name)}>
                <span className="txt">
                  <span className="line">
                    <span className="ti">{m.title || m.name}</span>
                    {m.new && <span className="badge">{t("新")}</span>}
                    {!m.noDecay && m.state === "fuzzy" && <span className="fade">{t("快淡忘了")}</span>}
                    {!m.noDecay && m.state === "faded" && <span className="fade">{t("已淡忘")}</span>}
                  </span>
                  <span className="gx">
                    {t(TYPE_NAME[typeOf(m)])} · {m.galaxy || t("未归类")}
                    {filter.kind === "new" && m.createdAt ? t(" · 来自 {d} 的对话", { d: m.createdAt.slice(5) }) : ""}
                  </span>
                </span>
                {m.usedLastTurn && <i className="dot" title={t("上一轮已使用")} />}
              </button>
              {filter.kind === "new" ? (
                <span className="quick">
                  <button className="memchip" data-action="memory.confirm" data-target={m.name} disabled={busy.includes(m.name)} onClick={() => confirm(m.name)}>
                    <Icon name="check" size={13} />{t("对")}
                  </button>
                  <button className="memchip" data-action="memory.forget" data-target={m.name} disabled={busy.includes(m.name)} onClick={() => forget(m.name)}>
                    <Icon name="cross" size={13} />{t("忘掉")}
                  </button>
                </span>
              ) : null}
            </div>
          ))}
          {list.length > shown && (
            <button className="lnk more" data-action="memory.more" onClick={() => setShown(shown + PAGE)}>
              {t("再显示 {n} 条（共 {total} 条）", { n: Math.min(PAGE, list.length - shown), total: list.length })}
            </button>
          )}
        </div>
        {selected && (
          <MemoryDetail
            m={selected}
            edit={edit}
            setEdit={setEdit}
            busy={busy.includes(selected.name)}
            galaxies={galaxies.map(([g]) => g)}
            onSave={save}
            onConfirm={() => confirm(selected.name)}
            onForget={() => forget(selected.name)}
            onHistory={() => void openHistory(selected.name)}
            historyOpen={showPast === selected.name}
          >
            {showPast === selected.name && (
              <History list={past[selected.name]} current={selected.revision ?? 1} busy={busy.includes(selected.name)}
                onRestore={(rev) => restore(selected.name, rev)} />
            )}
          </MemoryDetail>
        )}
      </div>
      {error && <div className="why">{error}</div>}
      {authoring}
    </div>
  );
}

function clip(s: string): string {
  const v = s.trim();
  return v.length > 24 ? v.slice(0, 24) + "…" : v;
}
