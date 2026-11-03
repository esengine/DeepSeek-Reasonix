import { useState } from "react";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import type { AgentPort, BackupApplyResult, BackupCategory, BackupEntry, BackupPlan, BackupPlanItem, BackupPluginRef } from "../port/port";
import { AddPlugin } from "./AddPlugin";

export const CATEGORY_LABEL: Record<BackupCategory, string> = {
  settings: "backupRestore.modelsAndInterface",
  extensions: "backupRestore.skillsPluginsAndMCP",
  memory: "backupRestore.longTermMemory",
  automation: "backupRestore.hooksAndStatusLineCommand",
  secrets: "backupRestore.aPIKeys",
};

const STATUS_LABEL: Record<BackupPlanItem["status"], string> = {
  new: "backupRestore.notOnThisMachine",
  changed: "backupRestore.differsFromThisMachine",
  same: "backupRestore.sameAsThisMachine",
  install: "backupRestore.needsInstall",
};

const CONSENT_LABEL: Record<string, string> = {
  executes: "backupRestore.iHaveReadThisCommand",
  endpoint: "backupRestore.iAllowConversationsAndThe",
  replaces_secret: "backupRestore.iAllowTheKeyIn",
  imports: "backupRestore.iHaveReadThisInstruction",
};

// The preview is the kernel's: which rows need consent, and why, is decided
// there and rechecked on apply. This panel only collects two separate answers
// per row — take it, and (where asked) allow what it does.
// Where a restore reads from: an account's backup or a file the person opened.
// The passphrase is asked for here either way, and apply goes to the route that
// holds the plan the preview made.
export interface RestoreSource {
  name: string;
  preview: (passphrase: string) => Promise<BackupPlan>;
  apply: (planId: string, items: string[], consented: string[]) => Promise<BackupApplyResult>;
  // Which rows start ticked; the kernel's recommendation when absent.
  preselect?: (item: BackupPlanItem) => boolean;
}

export function accountBackupSource(port: AgentPort, backup: BackupEntry): RestoreSource {
  return {
    name: backup.label || t("common.untitledBackup"),
    preview: (passphrase) => port.previewBackup(backup.id, passphrase),
    apply: (planId, items, consented) => port.applyBackup(planId, items, consented),
  };
}

export function BackupRestore({ port, source, onClose }: { port: AgentPort; source: RestoreSource; onClose: () => void }) {
  const [pass, setPass] = useState("");
  const [plan, setPlan] = useState<BackupPlan | null>(null);
  const [picked, setPicked] = useState<Set<string>>(new Set());
  const [allowed, setAllowed] = useState<Set<string>>(new Set());
  const [result, setResult] = useState<BackupApplyResult | null>(null);
  const [installing, setInstalling] = useState<BackupPluginRef | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  const open = async () => {
    setBusy(true);
    setError("");
    try {
      const p = await source.preview(pass);
      setPlan(p);
      setPicked(new Set(p.items.filter(source.preselect ?? ((i) => i.recommended)).map((i) => i.id)));
      setAllowed(new Set());
      setPass("");
    } catch (e) {
      setError(reason(e));
    } finally {
      setBusy(false);
    }
  };

  const flip = (set: Set<string>, id: string) => {
    const next = new Set(set);
    if (next.has(id)) next.delete(id);
    else next.add(id);
    return next;
  };

  const blocked = plan?.items.filter((i) => picked.has(i.id) && i.consent && !allowed.has(i.id)) ?? [];

  const apply = async () => {
    if (!plan) return;
    setBusy(true);
    setError("");
    try {
      const ids = plan.items.filter((i) => picked.has(i.id)).map((i) => i.id);
      const consented = ids.filter((id) => allowed.has(id));
      setResult(await source.apply(plan.planId, ids, consented));
    } catch (e) {
      setError(reason(e));
    } finally {
      setBusy(false);
    }
  };

  if (result) {
    return (
      <div className="bk-restore" data-action="backup.result">
        <p className="acct-note">{t("backupRestore.restoredItems", { n: result.applied.length })}</p>
        {result.failed?.map((f) => (
          <p key={f.id} className="acct-note" data-err="">
            {f.id}：{f.error}
          </p>
        ))}
        {result.reloadError && <p className="acct-note">{t("backupRestore.theSetupWasWrittenBut", { e: result.reloadError })}</p>}
        {!!result.plugins?.length && (
          <>
            <p className="acct-note">{t("backupRestore.thesePluginsGoThroughThe")}</p>
            <ul className="bk-list">
              {result.plugins.map((p) => (
                <li key={p.name} className="bk-row">
                  <div className="who">
                    <span className="nm">{p.name}</span>
                    <code className="meta">{p.source}{p.version ? " · " + p.version : ""}</code>
                  </div>
                  <button className="btn sm" data-action="backup.install-plugin" data-target={p.name} disabled={installing?.name === p.name} onClick={() => setInstalling({ ...p })}>
                    {t("backupRestore.install")}
                  </button>
                </li>
              ))}
            </ul>
          </>
        )}
        <div className="bk-acts">
          <button className="act" data-action="backup.done" onClick={onClose}>{t("common.done2")}</button>
        </div>
        {installing && (
          <AddPlugin key={installing.name} port={port} source={installing.source} onClose={() => setInstalling(null)} onInstalled={() => setInstalling((current) => current === installing ? null : current)} />
        )}
      </div>
    );
  }

  if (!plan) {
    return (
      <div className="bk-restore">
        <p className="acct-note">{t("backupRestore.enterThePassphraseForTo", { name: source.name })}</p>
        <div className="bk-fields">
          <label className="grow">
            <span>{t("backupRestore.passphrase")}</span>
            <input data-action="backup.passphrase" data-target="open" type="password" autoComplete="current-password" disabled={busy} value={pass} onChange={(e) => setPass(e.target.value)} />
          </label>
        </div>
        <div className="bk-acts">
          <button className="act" data-primary data-action="backup.preview" disabled={!pass || busy} onClick={() => void open()}>
            {t(busy ? "backupRestore.decrypting" : "backupRestore.previewDifferences")}
          </button>
          <button className="act" data-action="backup.cancel" onClick={onClose}>{t("common.cancel")}</button>
        </div>
        {error && <p className="acct-note" data-err="">{error}</p>}
      </div>
    );
  }

  return (
    <div className="bk-restore">
      {!plan.samePlatform && (
        <div className="find" data-lvl="warn" role="note">
          <span className="t">{t("backupRestore.thisBackupComesFrom", { p: plan.platform })}</span>
          <span className="why">{t("backupRestore.commandsAndPathsMayNot")}</span>
        </div>
      )}
      <ul className="bk-plan">
        {plan.items.map((it) => (
          <li key={it.id} className="bk-item" data-status={it.status} data-consent={it.consent ? "" : undefined}>
            <label className="take">
              <input type="checkbox" disabled={busy} checked={picked.has(it.id)} onChange={() => setPicked((s) => flip(s, it.id))} data-action="backup.pick" data-target={it.id} />
              <span className="cat">{t(CATEGORY_LABEL[it.category])}</span>
              <span className="nm">{it.name}</span>
              <span className="st">{t(STATUS_LABEL[it.status])}</span>
            </label>
            {it.summary && <code className="sum">{it.summary}</code>}
            {it.details?.map((d) => (
              <code key={d} className="sum">{d}</code>
            ))}
            {it.content !== undefined && it.content !== "" && <pre className="body">{it.content}</pre>}
            {it.previous && <span className="prev">{t("backupRestore.hereNow")}<code>{it.previous}</code></span>}
            {!!it.files && <span className="prev">{t("common.files", { n: it.files })}</span>}
            {it.paths?.filter((p) => !p.exists).map((p) => (
              <span key={p.path} className="warn">{t("backupRestore.missingHere", { p: p.path })}</span>
            ))}
            {it.crossPlatform && <span className="warn">{t("backupRestore.fromAnotherSystemItMay")}</span>}
            {it.consent && picked.has(it.id) && (
              <label className="allow">
                <input type="checkbox" disabled={busy} checked={allowed.has(it.id)} onChange={() => setAllowed((s) => flip(s, it.id))} data-action="backup.allow" data-target={it.id} />
                <span>{t(CONSENT_LABEL[it.consent])}</span>
              </label>
            )}
          </li>
        ))}
      </ul>
      {!!plan.omitted?.length && <p className="acct-note">{t("backupRestore.filesWereTooLargeOr", { n: plan.omitted.length })}</p>}
      <div className="bk-acts">
        <button className="act" data-primary data-action="backup.apply" disabled={busy || picked.size === 0 || blocked.length > 0} onClick={() => void apply()}>
          {t(busy ? "backupRestore.restoring" : "backupRestore.restoreSelected", { n: picked.size })}
        </button>
        <button className="act" data-action="backup.cancel" onClick={onClose}>{t("common.cancel")}</button>
        {blocked.length > 0 && <span className="acct-note">{t("backupRestore.itemsStillNeedConfirmation", { n: blocked.length })}</span>}
      </div>
      {error && <p className="acct-note" data-err="">{error}</p>}
    </div>
  );
}
