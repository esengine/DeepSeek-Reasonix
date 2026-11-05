import { useCallback, useEffect, useRef, useState } from "react";
import { Clip } from "./Clip";
import { useEscape } from "./dismiss";
import { t } from "../i18n";
import type { Protocol, ProviderCheck, ProviderCheckDraft, ProviderDraft, ProviderEdit, ProviderEntry, ProviderModelCheck, ProviderModelCheckRequest, ProviderProbe, ProviderRemoval, ProviderPreset, ProviderPresetInstall } from "../port/port";
import { AddProvider } from "./AddProvider";
import { ProviderDetail } from "./ProviderDetail";
import { accountKey, accountLabel, disambiguate, hostOf } from "./vendors";
import { reason } from "../i18n/kernel";
import { providerDot } from "./providerdot";
import { ServiceMark } from "./ServiceMark";
import { moveAccount, orderAccounts, useProviderOrder, writeProviderOrder } from "../state/providerorder";
import { StudioIcon } from "./StudioIcon";
import { RemovalNotice, removalChangedRoles, type Removal } from "./ProviderRemoval";

// A connection is an account, not a config row. One endpoint answering two
// protocols is two rows in the file and one service to the person paying for it,
// so the rows group by host and the protocol becomes a switch on the account.
//
// Adding one is still two questions — where and with what key — because the rest
// is knowable by asking the endpoint.

export type Port = {
  providers(): Promise<ProviderEntry[]>;
  protocols(): Promise<Protocol[]>;
  providerPresets(): Promise<ProviderPreset[]>;
  installProviderPreset(req: ProviderPresetInstall): Promise<{ names: string[] }>;
  probeProvider(baseUrl: string, apiKey: string): Promise<ProviderProbe>;
  saveProvider(draft: ProviderDraft): Promise<void>;
  removeProvider(name: string): Promise<ProviderRemoval>;
  checkProvider(name: string, draft?: ProviderCheckDraft): Promise<ProviderCheck>;
  checkProviderModel(request: ProviderModelCheckRequest): Promise<ProviderModelCheck>;
  editProvider(edit: ProviderEdit): Promise<void>;
  renameProvider(names: string[], displayName: string): Promise<void>;
  openExternal(url: string): Promise<void>;
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

export function groupAccounts(list: ProviderEntry[]): Account[] {
  const out = new Map<string, Account>();
  for (const p of list) {
    const host = hostOf(p.baseUrl);
    const kind = p.kind || "openai";
    let key = accountKey(host, p.keyEnv);
    // A second entry for a door the account already has is a duplicate, and a
    // duplicate is still somebody's config: it gets its own row, never a seat
    // behind the first one.
    if (out.get(key)?.byKind[kind]) key = `${key}\u0000${p.name}`;
    let a = out.get(key);
    if (!a) {
      a = { key, label: "", host, hint: p.name, derived: "", byKind: {}, kinds: [] };
      out.set(key, a);
    }
    a.byKind[kind] = p;
    a.kinds.push(kind);
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
  const [removal, setRemoval] = useState<Removal | null>(null);
  const [picked, setPickedRaw] = useState("");
  const setPicked = useCallback((key: string) => { setRemoval(null); setPickedRaw(key); }, []);
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
    setRemoval(null);
    const key = selected;
    try {
      const report = await port.removeProvider(name);
      if (removalChangedRoles(report)) setRemoval({ key, name, why: "", report });
    } catch (e) {
      setRemoval({ key, name, why: reason(e), report: null });
    } finally {
      reload();
      onChanged();
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
    else onFailed(t("models.could_not_save_the_service"));
  };

  if (list === null) return <p className="acct-note">{t("common.loading")}</p>;

  const query = q.trim().toLowerCase();
  const shown = accounts.filter((a) => !query || a.label.toLowerCase().includes(query) || a.host.toLowerCase().includes(query));
  const current = accounts.find((a) => a.key === selected);
  return (
    <div className="psplit">
      <div className="plist">
        {accounts.length >= SEARCH_FROM && (
          <input className="psearch" type="search" value={q} spellCheck={false}
            placeholder={t("models.search_added_services")} aria-label={t("models.search_added_services")}
            onChange={(e) => setQ(e.target.value)} />
        )}
        <div className="plist-rows" role="group" aria-label={t("models.n_sources", { n: accounts.length })}>
          {shown.map((a, index) => {
            const entries = Object.values(a.byKind);
            const dot = providerDot(entries);
            if (renaming === a.key) {
              return (
                <div className="svcrow-wrap" data-selected="true" data-renaming="" key={a.key}>
                  <input className="svcrow-name" autoFocus maxLength={DISPLAY_NAME_MAX} spellCheck={false}
                    defaultValue={shownName(a) || a.derived} placeholder={a.derived}
                    aria-label={t("models.rename_name", { name: a.label })}
                    title={t("models.enter_saves_esc_cancels_leave", { name: a.derived })}
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
                  <ServiceMark mark={Object.values(a.byKind).find((e) => e.mark)?.mark} label={a.label} id={a.key} />
                  <span className="tx">
                    <span className="nm">{a.label}</span>
                    <Clip className="ds">{a.host}</Clip>
                  </span>
                  <i className="pstate" data-state={dot.state} title={dot.title} />
                </button>
                <span className="svcrow-order">
                  <button data-action="provider.rename-start" data-target={a.key} disabled={busy !== ""}
                    aria-label={t("models.rename_name_f2", { name: a.label })}
                    title={t("models.rename_name_f2", { name: a.label })}
                    onClick={() => startRename(a.key)}><StudioIcon name="edit" /></button>
                  {index > 0 && <button data-action="provider.move" data-target={a.key} data-value="up"
                    aria-label={t("models.move_name_up_alt_up", { name: a.label })}
                    title={t("models.move_name_up_alt_up", { name: a.label })}
                    onClick={() => move(a.key, -1)}><StudioIcon name="arrow" /></button>}
                  {index < shown.length - 1 && <button data-action="provider.move" data-target={a.key} data-value="down"
                    aria-label={t("models.move_name_down_alt_down", { name: a.label })}
                    title={t("models.move_name_down_alt_down", { name: a.label })}
                    onClick={() => move(a.key, 1)}><StudioIcon name="arrow" className="svcrow-arrow-down" /></button>}
                </span>
              </div>
            );
          })}
          {accounts.length === 0 && <div className="empty">{t("models.no_model_sources_configured_yet")}</div>}
          {accounts.length > 0 && shown.length === 0 && <div className="empty">{t("models.no_matching_service")}</div>}
        </div>
        <button className="act padd" data-action="provider.add-start" aria-pressed={adding} onClick={() => leave(() => setAdding(true))}>
          <b aria-hidden="true">＋</b>{t("models.add_model_service")}
        </button>
      </div>
      <div className="pmain" ref={pmain}>
        {removal && (removal.report || !accounts.some((a) => a.key === removal.key)) && (
          <RemovalNotice removal={removal} />
        )}
        {asked && (
          <div className="wsconfirm" role="alertdialog" aria-labelledby="provider-leave-q" aria-describedby="provider-leave-q"
            data-action-keydown="layer.dismiss"
            onKeyDown={(ev) => {
              if (ev.key !== "Escape") return;
              ev.stopPropagation();
              keepEditing();
            }}>
            <div className="wsconfirm-t">
              <span className="q" id="provider-leave-q">{t("models.this_service_has_unsaved_changes")}</span>
              <span className="h">{t("common.they_will_be_lost_if")}</span>
            </div>
            <div className="wsconfirm-a">
              <button autoFocus data-primary data-action="layer.dismiss" onClick={keepEditing}>{t("models.keep_editing")}</button>
              <button data-action="provider.discard-edits" data-danger
                onClick={() => { const { go } = asked; setAsked(null); setDirty(false); go(); }}>
                {t("models.discard_changes_and_leave")}
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
            onRemove={remove} removeFailed={removal?.key === current.key ? removal.why : ""}
            onRename={() => startRename(current.key)}
            declare={declare}
            onEdited={() => { const fresh = reload(); onChanged(); return fresh; }}
            onChecked={() => void reload()}
            onDirty={setDirty} />
        ) : (
          <div className="empty">{t("models.add_a_model_service_to")}</div>
        )}
      </div>
    </div>
  );
}
