import { useCallback, useEffect, useRef, useState } from "react";
import { Clip } from "./Clip";
import { useEscape } from "./dismiss";
import { t } from "../i18n";
import type { Protocol, ProviderCheck, ProviderDraft, ProviderEdit, ProviderEntry, ProviderModelCheck, ProviderModelCheckRequest, ProviderProbe } from "../port/port";
import { AddProvider } from "./AddProvider";
import { ProviderDetail } from "./ProviderDetail";
import { accountKey, accountLabel, disambiguate, hostOf } from "./vendors";
import { reason } from "../i18n/kernel";
import { moveAccount, orderAccounts, useProviderOrder, writeProviderOrder } from "../state/providerorder";
import { StudioIcon } from "./StudioIcon";

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
  saveProvider(draft: ProviderDraft): Promise<void>;
  removeProvider(name: string): Promise<void>;
  checkProvider(name: string): Promise<ProviderCheck>;
  checkProviderModel(request: ProviderModelCheckRequest): Promise<ProviderModelCheck>;
  editProvider(edit: ProviderEdit): Promise<void>;
  setProviderWebSearch(name: string, on: boolean): Promise<void>;
  setProviderThinking(name: string, on: boolean): Promise<void>;
  setProviderContinuation(name: string, mode: string): Promise<void>;
  renameProvider(names: string[], displayName: string): Promise<void>;
};

// One account: every configured entry that answers on the same host.
export interface Account {
  key: string;
  label: string;
  host: string;
  // The config entry's own name, shown only when one host holds two accounts.
  hint: string;
  // What the account is called when nobody has renamed it.
  derived: string;
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
      a = { key, label: "", host, hint: p.name, derived: "", byKind: {}, kinds: [] };
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
  for (const a of out.values()) {
    const entries = Object.values(a.byKind);
    a.label = accountLabel(a.host, entries);
    a.derived = accountLabel(a.host, entries.map((e) => ({ ...e, displayName: undefined })));
  }
  return disambiguate([...out.values()]);
}

interface ProvidersProps {
  port: Port;
  onChanged: () => void;
  // A switch that was refused has to say so; silence reads as a click that
  // missed, and the page above already has one place to say it.
  onFailed: (why: string) => void;
  // Which protocol each account is showing, and how to change it. The model
  // list reads the same map, so switching here re-lists the models there.
  protocol: Record<string, string>;
  onProtocol: (account: Account, kind: string) => void;
  activeKindFor: (account: Account) => string;
  // The source whose editor opens on its reasoning fields, when the composer
  // sent the user here to declare effort levels.
  declare?: string;
}

const SEARCH_FROM = 6;
// The kernel counts characters; maxLength counts UTF-16 units, so this can only
// stop short of the kernel's limit, never past it.
const DISPLAY_NAME_MAX = 64;

const shownName = (a: Account) => Object.values(a.byKind).find((e) => e.displayName?.trim())?.displayName?.trim() ?? "";

// A list of accounts beside the one being edited: picking a row is navigation,
// and every field of the picked account is on screen without a second click.
export function Providers({ port, onChanged, onFailed, protocol, onProtocol, activeKindFor, declare }: ProvidersProps) {
  const [list, setList] = useState<ProviderEntry[] | null>(null);
  const [adding, setAdding] = useState(false);
  useEscape(adding, () => setAdding(false));
  const [busy, setBusy] = useState("");
  const [picked, setPicked] = useState("");
  const [q, setQ] = useState("");
  const [renaming, setRenaming] = useState("");
  const [dirty, setDirty] = useState(false);
  const [asked, setAsked] = useState<{ go: () => void; from: HTMLElement | null } | null>(null);
  const pmain = useRef<HTMLDivElement>(null);
  const order = useProviderOrder();
  const rows = useRef(new Map<string, HTMLButtonElement>());
  // The accounts that existed when an add began: the one that is new afterwards
  // is the one just added, and it is what the detail should show.
  const before = useRef<Set<string> | null>(null);

  const reload = useCallback(
    () => port.providers().then(setList).catch(() => setList([])),
    [port],
  );
  useEffect(() => { void reload(); }, [reload]);

  const accounts = list ? orderAccounts(groupAccounts(list), order) : [];
  useEffect(() => {
    if (!list || !before.current) return;
    const fresh = groupAccounts(list).find((a) => !before.current?.has(a.key));
    before.current = null;
    if (fresh) setPicked(fresh.key);
  }, [list]);
  // Until someone picks, the page shows the service it was sent to, else the
  // one in use, else the first.
  const selected = accounts.some((a) => a.key === picked) ? picked : (
    accounts.find((a) => declare && Object.values(a.byKind).some((e) => e.name === declare))
    ?? accounts.find((a) => Object.values(a.byKind).some((e) => e.inUse))
    ?? accounts[0]
  )?.key ?? "";

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

  // An empty name, or the one it would be called anyway, clears the label so
  // the account follows its derived name again.
  const rename = async (a: Account, typed: string) => {
    setRenaming("");
    const value = typed.trim();
    const next = value === a.derived ? "" : value;
    if (next === shownName(a)) return;
    const names = Object.values(a.byKind).map((e) => e.name);
    setBusy(`rename:${a.key}`);
    onFailed("");
    try {
      await port.renameProvider(names, next);
      reload();
      onChanged();
    } catch (e) {
      onFailed(reason(e));
    } finally {
      setBusy("");
    }
  };

  // A form with pending edits is replaced only after the person says so. A save
  // in flight is not interrupted: it lands on its own and the form is clean after.
  const leave = (go: () => void, leaving = true) => {
    if (!leaving || !dirty) go();
    else if (!busy.startsWith("edit:")) {
      const from = document.activeElement;
      setAsked({ go, from: from instanceof HTMLElement && from !== document.body ? from : null });
    }
  };
  useEffect(() => {
    if (!dirty) setAsked(null);
  }, [dirty]);

  // Focus goes back to the control that asked to leave, or to the form's first
  // field when that control is gone.
  const keepEditing = () => {
    const back = asked?.from?.isConnected ? asked.from : pmain.current?.querySelector<HTMLElement>("input:not(:disabled)");
    setAsked(null);
    back?.focus();
  };

  const startRename = (key: string) => leave(() => {
    setAdding(false);
    setPicked(key);
    setRenaming(key);
  }, key !== selected);

  const move = (key: string, direction: -1 | 1) => {
    const next = moveAccount(order, accounts.map((a) => a.key), key, direction);
    if (!next) return;
    if (!picked) setPicked(selected);
    rows.current.get(key)?.focus();
    if (writeProviderOrder(next)) onFailed("");
    else onFailed(t("无法保存服务顺序。"));
  };

  if (list === null) return <p className="acct-note">{t("正在读取…")}</p>;

  const query = q.trim().toLowerCase();
  const shown = accounts.filter((a) => !query || a.label.toLowerCase().includes(query) || a.host.toLowerCase().includes(query));
  const current = accounts.find((a) => a.key === selected);
  return (
    <div className="psplit">
      <div className="plist">
        {accounts.length >= SEARCH_FROM && (
          <input className="psearch" type="search" value={q} spellCheck={false}
            placeholder={t("搜索已添加的服务")} aria-label={t("搜索已添加的服务")}
            onChange={(e) => setQ(e.target.value)} />
        )}
        <div className="plist-rows" role="group" aria-label={t("{n} 个来源", { n: accounts.length })}>
          {shown.map((a, index) => {
            const entries = Object.values(a.byKind);
            const inUse = entries.some((e) => e.inUse);
            const keyless = entries.every((e) => !e.hasKey);
            if (renaming === a.key) {
              return (
                <div className="svcrow-wrap" data-selected="true" data-renaming="" key={a.key}>
                  <input className="svcrow-name" autoFocus maxLength={DISPLAY_NAME_MAX} spellCheck={false}
                    defaultValue={shownName(a) || a.derived} placeholder={a.derived}
                    aria-label={t("重命名 {name}", { name: a.label })}
                    title={t("回车保存，Esc 取消；留空恢复为「{name}」", { name: a.derived })}
                    onFocus={(ev) => ev.currentTarget.select()}
                    onBlur={(ev) => void rename(a, ev.currentTarget.value)}
                    data-action-keydown="provider.rename" data-target={a.key}
                    onKeyDown={(ev) => {
                      if (ev.key === "Enter") {
                        ev.preventDefault();
                        ev.currentTarget.blur();
                      } else if (ev.key === "Escape") {
                        // Abandoning a rename is not closing the settings page.
                        ev.stopPropagation();
                        ev.currentTarget.value = shownName(a) || a.derived;
                        ev.currentTarget.blur();
                      }
                    }} />
                </div>
              );
            }
            return (
              <div className="svcrow-wrap" data-selected={!adding && a.key === selected} key={a.key}>
                <button className="svcrow" data-action-click="provider.select" data-action-keydown="provider.move" data-target={a.key}
                  ref={(node) => { if (node) rows.current.set(a.key, node); else rows.current.delete(a.key); }}
                  aria-pressed={!adding && a.key === selected}
                  onKeyDown={(event) => {
                    if (event.key === "F2") {
                      event.preventDefault();
                      startRename(a.key);
                      return;
                    }
                    if (!query && event.altKey && !event.ctrlKey && !event.metaKey && !event.shiftKey
                      && (event.key === "ArrowUp" || event.key === "ArrowDown")) {
                      event.preventDefault();
                      move(a.key, event.key === "ArrowUp" ? -1 : 1);
                    }
                  }}
                  onClick={() => leave(() => { setAdding(false); setPicked(a.key); }, a.key !== selected)}>
                  <span className="tx">
                    <span className="nm">{a.label}</span>
                    <Clip className="ds">{a.host}</Clip>
                  </span>
                  <i className="pstate" data-state={inUse ? "use" : keyless ? "warn" : undefined}
                    title={t(inUse ? "正在用" : keyless ? "缺 key" : "")} />
                </button>
                <span className="svcrow-order">
                  <button data-action="provider.rename-start" data-target={a.key} disabled={busy !== ""}
                    aria-label={t("重命名 {name}（F2）", { name: a.label })}
                    title={t("重命名 {name}（F2）", { name: a.label })}
                    onClick={() => startRename(a.key)}><StudioIcon name="edit" /></button>
                  {index > 0 && <button data-action="provider.move" data-target={a.key} data-value="up"
                    aria-label={t("上移 {name}（Alt+上方向键）", { name: a.label })}
                    title={t("上移 {name}（Alt+上方向键）", { name: a.label })}
                    onClick={() => move(a.key, -1)}><StudioIcon name="arrow" /></button>}
                  {index < shown.length - 1 && <button data-action="provider.move" data-target={a.key} data-value="down"
                    aria-label={t("下移 {name}（Alt+下方向键）", { name: a.label })}
                    title={t("下移 {name}（Alt+下方向键）", { name: a.label })}
                    onClick={() => move(a.key, 1)}><StudioIcon name="arrow" className="svcrow-arrow-down" /></button>}
                </span>
              </div>
            );
          })}
          {accounts.length === 0 && <div className="empty">{t("尚未配置任何模型来源。")}</div>}
          {accounts.length > 0 && shown.length === 0 && <div className="empty">{t("没有匹配的服务。")}</div>}
        </div>
        <button className="act padd" data-action="provider.add-start" aria-pressed={adding} onClick={() => leave(() => setAdding(true))}>
          <b aria-hidden="true">＋</b>{t("添加模型服务")}
        </button>
      </div>
      <div className="pmain" ref={pmain}>
        {asked && (
          <div className="wsconfirm" role="alertdialog" aria-labelledby="provider-leave-q" aria-describedby="provider-leave-q"
            data-action-keydown="layer.dismiss"
            onKeyDown={(ev) => {
              if (ev.key !== "Escape") return;
              ev.stopPropagation();
              keepEditing();
            }}>
            <div className="wsconfirm-t">
              <span className="q" id="provider-leave-q">{t("这个服务有未保存的更改")}</span>
              <span className="h">{t("离开后这些更改会丢失。")}</span>
            </div>
            <div className="wsconfirm-a">
              <button autoFocus data-primary data-action="layer.dismiss" onClick={keepEditing}>{t("保留编辑")}</button>
              <button data-action="provider.discard-edits" data-danger
                onClick={() => { const { go } = asked; setAsked(null); setDirty(false); go(); }}>
                {t("放弃更改并离开")}
              </button>
            </div>
          </div>
        )}
        {adding ? (
          <AddProvider
            port={port}
            taken={list.map((p) => p.name)}
            known={list}
            onDone={() => {
              before.current = new Set(accounts.map((a) => a.key));
              setAdding(false);
              void reload();
              onChanged();
            }}
            onCancel={() => setAdding(false)}
          />
        ) : current ? (
          <ProviderDetail key={current.key} a={current} port={port} busy={busy} setBusy={setBusy}
            kind={protocol[current.key] ?? activeKindFor(current)}
            onProtocol={(k) => leave(() => onProtocol(current, k))}
            onRemove={remove}
            onRename={() => startRename(current.key)}
            declare={declare}
            onEdited={() => { const fresh = reload(); onChanged(); return fresh; }}
            onDirty={setDirty}
            onFailed={onFailed} />
        ) : (
          <div className="empty">{t("添加一个模型服务后，在这里查看和修改它。")}</div>
        )}
      </div>
    </div>
  );
}
