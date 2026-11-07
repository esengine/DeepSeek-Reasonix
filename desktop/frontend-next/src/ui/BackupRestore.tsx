import { useState, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import type { AgentPort, BackupApplyResult, BackupCategory, BackupEntry, BackupPlan, BackupPlanItem, BackupPluginRef } from "../port/port";
import { AddPlugin } from "./AddPlugin";
import { StudioIcon } from "./StudioIcon";

export const CATEGORY_LABEL: Record<BackupCategory, string> = {
  settings: "data.models_and_interface",
  extensions: "data.skills_plugins_and_mcp",
  memory: "data.long_term_memory",
  automation: "data.hooks_and_status_line_command",
  secrets: "data.api_keys",
};

const STATUS_LABEL: Record<BackupPlanItem["status"], string> = {
  new: "data.not_on_this_machine",
  changed: "data.differs_from_this_machine",
  same: "data.same_as_this_machine",
  install: "data.needs_install",
};

const CONSENT_LABEL: Record<string, string> = {
  executes: "data.i_have_read_this_command",
  endpoint: "data.i_allow_conversations_and_the",
  replaces_secret: "data.i_allow_the_key_in",
  imports: "data.i_have_read_this_instruction",
};

// A restore is asked for and read in a dialog; what it did is said afterwards,
// in the page, in one strip per outcome.
function RestoreDialog({ title, onClose, footer, wide, children }: { title: string; onClose: () => void; footer: ReactNode; wide?: boolean; children: ReactNode }) {
  return createPortal(
    <div className="ns-veil" data-action-mousedown="layer.dismiss" onMouseDown={(e) => { if (e.target === e.currentTarget) onClose(); }}>
      <div className="ns-dlg bk-dlg" data-wide={wide ? "" : undefined} role="dialog" aria-modal="true" aria-label={title} data-action-keydown="layer.dismiss"
        onKeyDown={(e) => { if (e.key === "Escape") { e.stopPropagation(); onClose(); } }}>
        <header>
          <h3>{title}</h3>
          <button type="button" className="ns-x" data-action="layer.dismiss" aria-label={t("common.close")} title={t("common.close")} onClick={onClose}><StudioIcon name="close" /></button>
        </header>
        <div className="bk-dlg-body">{children}</div>
        <footer>{footer}</footer>
      </div>
    </div>,
    document.body,
  );
}

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
    name: backup.label || t("data.untitled_backup"),
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
      <div className="bk-result" data-action="backup.result">
        <div className="bk-strip" data-tone="ok">
          <StudioIcon name="check" /><span>{t("data.restored_n_items", { n: result.applied.length })}</span>
          <button type="button" className="ns-cancel" data-action="backup.done" onClick={onClose}>{t("common.done")}</button>
        </div>
        {result.failed?.map((f) => (
          <div key={f.id} className="bk-strip" data-tone="warn" role="alert"><StudioIcon name="warning" /><span>{f.id}：{f.error}</span></div>
        ))}
        {result.reloadError && <div className="bk-strip" data-tone="warn"><StudioIcon name="warning" /><span>{t("data.the_setup_was_written_but", { e: result.reloadError })}</span></div>}
        {!!result.plugins?.length && (
          <div className="bk-strip" data-tone="plain" data-plugins="">
            <span>{t("data.these_plugins_go_through_the")}</span>
            {result.plugins.map((p) => (
              <span key={p.name} className="bk-plug">
                <code>{p.name}{p.version ? " " + p.version : ""}</code>
                <button className="btn sm" data-action="backup.install-plugin" data-target={p.name} disabled={installing?.name === p.name} onClick={() => setInstalling({ ...p })}>
                  {t("data.install")}
                </button>
              </span>
            ))}
          </div>
        )}
        {installing && (
          <AddPlugin key={installing.name} port={port} source={installing.source} onClose={() => setInstalling(null)} onInstalled={() => setInstalling((current) => current === installing ? null : current)} />
        )}
      </div>
    );
  }

  if (!plan) {
    return (
      <RestoreDialog title={t("data.restore_a_backup")} onClose={onClose} footer={<>
        <button type="button" className="ns-cancel" data-action="backup.cancel" onClick={onClose}>{t("common.cancel")}</button>
        <button type="button" className="btn" data-primary data-action="backup.preview" disabled={!pass || busy} onClick={() => void open()}>
          <StudioIcon name="lock" />{t(busy ? "data.decrypting" : "data.preview_differences")}
        </button>
      </>}>
        <p className="acct-note">{t("data.enter_the_passphrase_for_name", { name: source.name })}</p>
        <div className="bk-fields" data-one="">
          <label className="grow">
            <span>{t("data.passphrase")}</span>
            <input autoFocus data-action-change="backup.passphrase" data-action-keydown="backup.restore" data-target="open" type="password" autoComplete="current-password" disabled={busy} value={pass} onChange={(e) => setPass(e.target.value)}
              onKeyDown={(e) => { if (e.key === "Enter" && pass && !busy) void open(); }} />
          </label>
        </div>
        {error && <p className="acct-note" data-err="">{error}</p>}
      </RestoreDialog>
    );
  }

  return (
    <RestoreDialog title={t("data.restore_a_backup")} onClose={onClose} wide footer={<>
      {blocked.length > 0 && <span className="acct-note">{t("data.n_items_still_need_confirmation", { n: blocked.length })}</span>}
      <button type="button" className="ns-cancel" data-action="backup.cancel" onClick={onClose}>{t("common.cancel")}</button>
      <button type="button" className="btn" data-primary data-action="backup.apply" disabled={busy || picked.size === 0 || blocked.length > 0} onClick={() => void apply()}>
        {t(busy ? "data.restoring" : "data.restore_n_selected", { n: picked.size })}
      </button>
    </>}>
      {!plan.samePlatform && (
        <div className="find" data-lvl="warn" role="note">
          <span className="t">{t("data.this_backup_comes_from_p", { p: plan.platform })}</span>
          <span className="why">{t("data.commands_and_paths_may_not")}</span>
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
            {it.previous && <span className="prev">{t("data.here_now")}<code>{it.previous}</code></span>}
            {!!it.files && <span className="prev">{t("common.n_files", { n: it.files })}</span>}
            {it.paths?.filter((p) => !p.exists).map((p) => (
              <span key={p.path} className="warn">{t("data.missing_here_p", { p: p.path })}</span>
            ))}
            {it.crossPlatform && <span className="warn">{t("data.from_another_system_it_may")}</span>}
            {it.consent && picked.has(it.id) && (
              <label className="allow">
                <input type="checkbox" disabled={busy} checked={allowed.has(it.id)} onChange={() => setAllowed((s) => flip(s, it.id))} data-action="backup.allow" data-target={it.id} />
                <span>{t(CONSENT_LABEL[it.consent])}</span>
              </label>
            )}
          </li>
        ))}
      </ul>
      {!!plan.omitted?.length && <p className="acct-note">{t("data.n_files_were_too_large", { n: plan.omitted.length })}</p>}
      {error && <p className="acct-note" data-err="">{error}</p>}
    </RestoreDialog>
  );
}
