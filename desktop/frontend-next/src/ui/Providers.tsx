import { useCallback, useEffect, useState } from "react";
import { t } from "../i18n";
import type { Protocol, ProviderCheck, ProviderEdit, ProviderEntry, ProviderProbe } from "../port/port";
import { AddProvider } from "./AddProvider";
import { EditConn } from "./EditConn";
import { KIND_LABEL, accountKey, accountLabel, disambiguate, hostOf } from "./vendors";
import { reason } from "../i18n/kernel";

// A connection is an account, not a config row. One endpoint answering two
// protocols is two rows in the file and one service to the person paying for it,
// so the rows group by host and the protocol becomes a switch on the account.
//
// Adding one is still two questions — where and with what key — because the rest
// is knowable by asking the endpoint.

export type Port = {
  providers(): Promise<ProviderEntry[]>;
  protocols(): Promise<Protocol[]>;
  probeProvider(baseUrl: string, apiKey: string): Promise<ProviderProbe>;
  saveProvider(draft: {
    name: string; kind: string; baseUrl: string; apiKey: string; models: string[];
    default: string; authHeader: boolean; noProxy: boolean; effort: string; vision: string[];
  }): Promise<void>;
  removeProvider(name: string): Promise<void>;
  checkProvider(name: string): Promise<ProviderCheck>;
  editProvider(edit: ProviderEdit): Promise<void>;
  setProviderWebSearch(name: string, on: boolean): Promise<void>;
  setProviderThinking(name: string, on: boolean): Promise<void>;
};

// One account: every configured entry that answers on the same host.
interface Account {
  key: string;
  label: string;
  host: string;
  // The config entry's own name, shown only when one host holds two accounts.
  hint: string;
  byKind: Record<string, ProviderEntry>;
  kinds: string[];
}

function groupAccounts(list: ProviderEntry[]): Account[] {
  const out = new Map<string, Account>();
  for (const p of list) {
    const host = hostOf(p.baseUrl);
    const key = accountKey(host, p.keyEnv);
    let a = out.get(key);
    if (!a) {
      a = { key, label: "", host, hint: p.name, byKind: {}, kinds: [] };
      out.set(key, a);
    }
    const kind = p.kind || "openai";
    if (!a.byKind[kind]) {
      a.byKind[kind] = p;
      a.kinds.push(kind);
    }
  }
  // Every door has to be in hand before the account can be named: the one the
  // user renamed is not always the first the config file lists.
  for (const a of out.values()) a.label = accountLabel(a.host, Object.values(a.byKind));
  return disambiguate([...out.values()]);
}

interface ProvidersProps {
  port: Port;
  onChanged: () => void;
  // A switch that was refused has to say so; silence reads as a click that
  // missed, and the page above already has one place to say it.
  onFailed: (why: string) => void;
  // Which protocol each account is showing, and how to change it. The model
  // list reads the same map, so switching here re-lists the models below.
  protocol: Record<string, string>;
  onProtocol: (account: Account, kind: string) => void;
  activeKindFor: (account: Account) => string;
}

export function Providers({ port, onChanged, onFailed, protocol, onProtocol, activeKindFor }: ProvidersProps) {
  const [list, setList] = useState<ProviderEntry[] | null>(null);
  const [adding, setAdding] = useState(false);
  const [busy, setBusy] = useState("");

  const reload = useCallback(() => {
    port.providers().then(setList).catch(() => setList([]));
  }, [port]);
  useEffect(reload, [reload]);

  const remove = async (name: string) => {
    setBusy(name);
    onFailed("");
    try {
      await port.removeProvider(name);
      reload();
      onChanged();
    } catch (e) {
      onFailed(reason(e));
    } finally {
      setBusy("");
    }
  };

  if (list === null) return <p className="acct-note">{t("正在读取…")}</p>;

  return (
    <>
      <div className="vlist">
        {groupAccounts(list).map((a) => (
          <Conn key={a.key} a={a} port={port} busy={busy} setBusy={setBusy}
            kind={protocol[a.key] ?? activeKindFor(a)}
            onProtocol={(k) => onProtocol(a, k)}
            onRemove={remove}
            onEdited={() => { reload(); onChanged(); }}
            onFailed={onFailed} />
        ))}
        {list.length === 0 && <div className="empty">{t("还没有配置任何模型来源。")}</div>}
      </div>
      {adding ? (
        <AddProvider
          port={port}
          taken={list.map((p) => p.name)}
          known={list}
          onDone={() => {
            setAdding(false);
            reload();
            onChanged();
          }}
          onCancel={() => setAdding(false)}
        />
      ) : (
        <button className="lnk" onClick={() => setAdding(true)}>
          {t("添加模型来源")}
        </button>
      )}
    </>
  );
}

// One account. The protocol is a switch on it rather than a fact on a row,
// because both entries are the same key at the same host; 测一下 is what turns
// "which protocol did we record" back into a finding when the endpoint moved.
function Conn({
  a, port, busy, setBusy, kind, onProtocol, onRemove, onEdited, onFailed,
}: {
  a: Account; port: Port; busy: string; setBusy: (b: string) => void;
  kind: string; onProtocol: (kind: string) => void; onRemove: (name: string) => void;
  onEdited: () => void; onFailed: (why: string) => void;
}) {
  const [found, setFound] = useState<ProviderCheck | null>(null);
  const [editing, setEditing] = useState(false);
  const entry = a.byKind[kind] ?? a.byKind[a.kinds[0]];
  const checking = busy === `check:${entry.name}`;
  const inUse = a.kinds.some((k) => a.byKind[k].inUse);

  const setSearch = async (on: boolean) => {
    setBusy(`search:${entry.name}`);
    onFailed("");
    try {
      await port.setProviderWebSearch(entry.name, on);
      onEdited();
    } catch (e) {
      onFailed(reason(e));
    } finally {
      setBusy("");
    }
  };

  const setThinking = async (on: boolean) => {
    setBusy(`thinking:${entry.name}`);
    onFailed("");
    try {
      await port.setProviderThinking(entry.name, on);
      onEdited();
    } catch (e) {
      onFailed(reason(e));
    } finally {
      setBusy("");
    }
  };

  const check = async () => {
    setBusy(`check:${entry.name}`);
    setFound(null);
    try {
      setFound(await port.checkProvider(entry.name));
    } catch (e) {
      setFound({ ok: false, error: reason(e) });
    } finally {
      setBusy("");
    }
  };

  const models = entry.models.length;
  // What the endpoint answered with that this connection does not list. The
  // vendor adds models to endpoints we already know; a stored list never finds
  // out on its own, and the probe is the one place that already has the answer.
  const unlisted = (found?.models ?? []).filter((m) => !entry.models.includes(m));
  return (
    <>
      <div className="vrow" data-on={inUse ? "" : undefined}>
        <span className="nm">{a.label}</span>
        <span className="ds">
          {a.host}
          {models > 0 ? ` · ${t("{n} 个模型", { n: models })}` : ""}
          {t(entry.hasKey ? "" : " · 缺 key")}
        </span>
        <span className="sc">{t(inUse ? "正在用" : "")}</span>
        {/* Hover-reveal is right for 删除; a diagnostic nobody can find is not
            a diagnostic, so this one stays on the row. */}
        <button className="sa lnk" data-keep onClick={() => setEditing((v) => !v)} disabled={busy !== ""}>
          {t(editing ? "收起" : "编辑")}
        </button>
        <button className="sa lnk" data-keep data-action="provider.probe" onClick={check} disabled={busy !== ""}>
          {t(checking ? "测试中…" : "测一下")}
        </button>
        {!entry.inUse && (
          <button className="sa lnk" data-action="provider.remove" data-target={entry.name} onClick={() => onRemove(entry.name)} disabled={busy !== ""}>
            {t("删除")}
          </button>
        )}
      </div>
      {a.kinds.length > 1 && (
        <div className="vway">
          <span className="lb">{t("接入方式")}</span>
          <div className="seg" role="group" aria-label={t("{name} 的接入方式", { name: a.label })}>
            {a.kinds.map((k) => (
              <button key={k} data-action="provider.protocol" data-target={entry.name} data-value={k} aria-pressed={k === kind} disabled={busy !== ""} onClick={() => onProtocol(k)}>
                {t(KIND_LABEL[k] ?? k)}
                {/* A door that carries a capability the other lacks has to say
                    so on itself: switching is otherwise a silent downgrade. */}
                {a.byKind[k].canWebSearch && <i className="perk">{t("联网搜索")}</i>}
              </button>
            ))}
          </div>
          <span className="why">
            {t(a.kinds.some((k) => a.byKind[k].canWebSearch) && !entry.canWebSearch ? "同一账号的两种接入方式。当前这一种不支持联网搜索；这是协议差异，不是可配置项。" : "同一账号的两种接入方式。切换后下方的模型列表随之改变。")}
          </span>
        </div>
      )}
      {entry.canWebSearch && (
        <div className="vway">
          <span className="lb">{t("联网搜索")}</span>
          <div className="seg" role="group" aria-label={t("{name} 的联网搜索", { name: a.label })}>
            {[true, false].map((on) => (
              <button key={String(on)} data-action="provider.web-search" data-target={entry.name} data-value={String(on)}
                aria-pressed={entry.webSearch === on} disabled={busy !== ""}
                onClick={() => setSearch(on)}>
                {t(on ? "开" : "关")}
              </button>
            ))}
          </div>
          <span className="why">{t("端点自己执行的搜索，不占本地工具。")}</span>
        </div>
      )}
      {entry.canSetThinking && (
        <div className="vway">
          <span className="lb">{t("思考参数")}</span>
          <div className="seg" role="group" aria-label={t("{name} 的思考参数", { name: a.label })}>
            {[true, false].map((on) => (
              <button key={String(on)} data-action="provider.thinking" data-target={entry.name} data-value={String(on)}
                aria-pressed={(entry.sendsThinking ?? true) === on} disabled={busy !== ""}
                onClick={() => setThinking(on)}>
                {t(on ? "自动" : "不发送")}
              </button>
            ))}
          </div>
          <span className="why">
            {t(entry.sendsThinking === false ? "只发送常规聊天参数，不再指定思考深度；模型自身的推理行为不受影响。" : "部分中转站不支持 thinking 字段，会拒绝整个请求。遇到这种情况请切换为「不发送」。")}
          </span>
        </div>
      )}
      {editing && (
        <EditConn
          entry={entry}
          port={port}
          busy={busy}
          setBusy={setBusy}
          onDone={() => {
            setEditing(false);
            onEdited();
          }}
        />
      )}
      {found && (
        <div className="find" data-lvl={found.ok ? "ok" : "warn"} role="status">
          <span className="t">
            {found.ok
              ? `${t("连上了")} · ${t(KIND_LABEL[found.kind ?? ""] ?? found.kind ?? "")} · ${t("{n} 个模型", { n: found.models?.length ?? 0 })}`
              : t("连不上")}
          </span>
          <span className="why">
            {!found.ok && found.error}
            {found.ok && found.matches === false &&
              t("记的是 {had}，但它答的是 {got}。", { had: t(KIND_LABEL[entry.kind] ?? entry.kind), got: t(KIND_LABEL[found.kind ?? ""] ?? found.kind ?? "") })}
            {found.ok && found.matches !== false && "key 有效，协议也对得上。"}
            {found.ok && found.noProxy && " 走代理连不上、直连可以。"}
            {/* A stored list cannot learn that the vendor shipped something. The
                probe already knows, so saying it costs nothing and is the only
                moment anyone finds out. */}
            {found.ok && unlisted.length > 0 &&
              " " + t("这个端点还有 {n} 个模型不在列表里：{names}。点「编辑」把它们加进来。", {
                n: unlisted.length,
                names: unlisted.slice(0, 3).join("、") + (unlisted.length > 3 ? "…" : ""),
              })}
          </span>
        </div>
      )}
    </>
  );
}

