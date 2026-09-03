import { useState } from "react";
import { asArray } from "../lib/array";
import { app } from "../lib/bridge";
import { useT } from "../lib/i18n";
import type { ProviderAccountView as AccountView } from "../lib/types";

export function normalizeProviderAccountView(p: AccountView): AccountView {
  return {
    ...p,
    providerId: String(p.providerId ?? ""),
    accountId: String(p.accountId ?? ""),
    label: String(p.label ?? ""),
    apiKeyEnv: String(p.apiKeyEnv ?? ""),
    enabled: p.enabled !== false,
    default: Boolean(p.default),
    keySet: Boolean(p.keySet),
    providerNames: asArray(p.providerNames),
    disabledRoutes: asArray(p.disabledRoutes),
  };
}

type ProviderPresetRef = { id: string; accountGroupId?: string; recommended?: boolean; displayOrder?: number };

export function accountsForProviderGroup(group: { id: string; providerGroup?: string; providers: { providerId?: string }[] }, accounts: AccountView[]): AccountView[] {
  const ids = new Set(group.providers.map((p) => p.providerId).filter(Boolean) as string[]);
  const groupID = accountGroupID(group);
  if (groupID) ids.add(groupID);
  return accounts.filter((account) => ids.has(account.providerId));
}

export function addAccountPresetID(group: { id: string; providerGroup?: string; providers: { providerId?: string }[] }, presets: ProviderPresetRef[]): string {
  const groupID = accountGroupID(group);
  if (!groupID) return "";
  const candidates = presets.filter((preset) => String(preset.accountGroupId ?? "").trim() === groupID);
  candidates.sort((a, b) => Number(Boolean(b.recommended)) - Number(Boolean(a.recommended))
    || Number(a.displayOrder ?? 0) - Number(b.displayOrder ?? 0)
    || a.id.localeCompare(b.id));
  return candidates[0]?.id ?? "";
}

function accountGroupID(group: { id: string; providerGroup?: string; providers: { providerId?: string }[] }): string {
  if (group.providerGroup) return group.providerGroup.trim();
  const providerID = group.providers.map((p) => p.providerId).find(Boolean);
  if (providerID) return providerID.trim();
  const [, suffix] = String(group.id ?? "").split(":", 2);
  return suffix?.trim() ?? "";
}

export function ProviderAccountManager({
  group,
  accounts,
  providerPresets,
  availableRoutes = [],
  busy,
  apply,
}: {
  group: { id: string; providerGroup?: string; providers: { providerId?: string }[] };
  accounts: AccountView[];
  providerPresets: ProviderPresetRef[];
  availableRoutes?: string[];
  busy: boolean;
  apply: (fn: () => Promise<unknown>) => Promise<unknown>;
}) {
  const t = useT();
  const [adding, setAdding] = useState(false);
  const [label, setLabel] = useState("");
  const [key, setKey] = useState("");
  const [renaming, setRenaming] = useState<string | null>(null);
  const [renameLabel, setRenameLabel] = useState("");
  const presetID = addAccountPresetID(group, providerPresets);
  if (accounts.length === 0 && !presetID) return null;

  return (
    <div className="provider-accounts">
      <div className="provider-card-block__label">{t("settings.providerAccounts")}</div>
      {accounts.map((account) => (
        <div key={`${account.providerId}/${account.accountId}`} className="provider-account-row">
          {renaming === account.accountId ? (
            <>
              <input
                className="input"
                value={renameLabel}
                onChange={(e) => setRenameLabel(e.target.value)}
                aria-label={t("settings.accountLabel")}
              />
              <button
                type="button"
                className="btn btn--small"
                disabled={busy || !renameLabel.trim()}
                onClick={() => apply(() => app.RenameProviderAccount(account.providerId, account.accountId, renameLabel.trim())).then(() => setRenaming(null))}
              >
                {t("common.save")}
              </button>
              <button type="button" className="btn btn--small" disabled={busy} onClick={() => setRenaming(null)}>{t("common.cancel")}</button>
            </>
          ) : (
            <>
              <strong>{account.label}</strong>
              {account.retired ? <span className="badge badge--feedback">{t("settings.accountRetire")}</span> : null}
              {account.default ? <span className="badge">{t("settings.accountDefault")}</span> : null}
              <span>{account.enabled ? t("settings.accountEnabled") : t("settings.accountDisabled")}</span>
              <span>{account.keySet ? t("settings.keySet") : t("settings.noKey")}</span>
              {asArray(account.disabledRoutes).length > 0 ? <span aria-label={t("settings.providerAccounts")}>{asArray(account.disabledRoutes).length}×</span> : null}
              {availableRoutes.length > 1 && !account.retired ? (
                <span className="provider-account-routes" role="group" aria-label={t("settings.providerAccounts")}>
                  {availableRoutes.map((route) => {
                    const disabled = asArray(account.disabledRoutes).includes(route);
                    return <button key={route} type="button" className="btn btn--small" disabled={busy} aria-pressed={!disabled}
                      onClick={() => apply(() => app.SetProviderAccountRouteEnabled(account.providerId, account.accountId, route, disabled))}>{route}</button>;
                  })}
                </span>
              ) : null}
              {account.retired ? (
                <button type="button" className="btn btn--small" disabled={busy} onClick={() => apply(() => app.RestoreProviderAccount(account.providerId, account.accountId))}>
                  {t("history.restore")}
                </button>
              ) : null}
              <button type="button" className="btn btn--small" disabled={busy || account.default || account.retired} onClick={() => apply(() => app.SetProviderAccountDefault(account.providerId, account.accountId))}>
                {t("settings.accountSetDefault")}
              </button>
              <button type="button" className="btn btn--small" disabled={busy || account.retired} onClick={() => apply(() => app.SetProviderAccountEnabled(account.providerId, account.accountId, !account.enabled))}>
                {account.enabled ? t("settings.accountDisable") : t("settings.accountEnable")}
              </button>
              <button
                type="button"
                className="btn btn--small"
                disabled={busy || account.retired}
                onClick={() => {
                  setRenaming(account.accountId);
                  setRenameLabel(account.label);
                }}
              >
                {t("common.edit")}
              </button>
              <button type="button" className="btn btn--small" disabled={busy || account.retired} onClick={() => {
                if (typeof window !== "undefined" && !window.confirm(t("settings.accountRetireConfirm"))) return;
                void apply(() => app.RetireProviderAccount(account.providerId, account.accountId));
              }}>
                {t("settings.accountRetire")}
              </button>
            </>
          )}
        </div>
      ))}
      {presetID && adding && (
        <div className="provider-account-add">
          <input className="input" value={label} onChange={(e) => setLabel(e.target.value)} placeholder={t("settings.accountLabel")} />
          <input className="input" type="password" value={key} onChange={(e) => setKey(e.target.value)} placeholder={t("settings.accountApiKey")} />
          <button
            type="button"
            className="btn btn--small"
            disabled={busy || !label.trim() || !key.trim()}
            onClick={() => apply(() => app.AddProviderPresetAccount(presetID, label.trim(), key)).then(() => {
              setAdding(false);
              setLabel("");
              setKey("");
            })}
          >
            {t("common.save")}
          </button>
          <button type="button" className="btn btn--small" disabled={busy} onClick={() => setAdding(false)}>{t("common.cancel")}</button>
        </div>
      )}
      {presetID && !adding && (
        <button type="button" className="btn btn--small" disabled={busy} onClick={() => setAdding(true)}>
          {t("settings.addAccount")}
        </button>
      )}
    </div>
    );
}
