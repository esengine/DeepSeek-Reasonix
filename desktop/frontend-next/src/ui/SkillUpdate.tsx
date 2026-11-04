import { useEffect, useState } from "react";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import type { AgentPort, SkillUpdateChange, SkillUpdatePlan } from "../port/port";
import { HttpError } from "../port/http_error";

const STALE = "skills.update_plan_stale";
const SIGN: Record<SkillUpdateChange["kind"], string> = { added: "+", changed: "~", removed: "−" };

// The plan an update of an installed skill would carry out, read before the
// person agrees. A script is a change they have to have opened; one the plan
// could not show is read from the source folder and said so.
export function SkillUpdate({ port, name, onClose, onDone }: { port: AgentPort; name: string; onClose: () => void; onDone: () => void }) {
  const [plan, setPlan] = useState<SkillUpdatePlan | null>(null);
  const [error, setError] = useState("");
  const [stale, setStale] = useState(false);
  const [busy, setBusy] = useState(false);
  const [opened, setOpened] = useState<Set<string>>(new Set());
  const [sureTruncated, setSureTruncated] = useState(false);
  const [sureOverwrite, setSureOverwrite] = useState(false);

  const load = () => {
    setPlan(null);
    setError("");
    setStale(false);
    setOpened(new Set());
    setSureTruncated(false);
    setSureOverwrite(false);
    port.planSkillUpdate(name).then(setPlan).catch((e) => setError(reason(e)));
  };
  useEffect(load, [port, name]); // eslint-disable-line react-hooks/exhaustive-deps

  if (error && !plan) return <div className="skupd mkt addpkg"><div className="why" role="alert">{error}</div><div className="acts"><button className="act" data-action="skills.update-close" onClick={onClose}>{t("common.close")}</button></div></div>;
  if (!plan) return <div className="skupd mkt addpkg" aria-busy="true"><div className="why">{t("common.loading_whats_new")}</div></div>;

  const code = plan.changes.filter((c) => c.code);
  const text = plan.changes.filter((c) => !c.code);
  const hidden = plan.changes.filter((c) => c.truncated || c.binary);
  const unread = code.filter((c) => !c.truncated && !c.binary && !opened.has(c.path)).length;
  const record = plan.changes.length === 0 && plan.state !== "synced";
  const ready = (plan.changes.length > 0 || record) && unread === 0 && (hidden.length === 0 || sureTruncated) && (!plan.overwritesLocal || sureOverwrite);

  const apply = async () => {
    setBusy(true);
    setError("");
    try {
      await port.applySkillUpdate(plan.name, plan.planId, sureOverwrite);
      onDone();
    } catch (e) {
      setStale(e instanceof HttpError && e.reason?.code === STALE);
      setError(reason(e));
    } finally {
      setBusy(false);
    }
  };

  const file = (c: SkillUpdateChange) => (
    <details className="cand" key={c.path} onToggle={(e) => { if ((e.currentTarget as HTMLDetailsElement).open) setOpened((s) => new Set(s).add(c.path)); }}>
      <summary className="cand-hd">
        <span className="nm">{SIGN[c.kind]} {c.path}</span>
        <span className="meta">{t(c.kind === "added" ? "ext.added" : c.kind === "removed" ? "common.delete" : "ext.changed")}</span>
      </summary>
      {c.truncated ? <div className="why">{t("ext.this_file_is_too_large", { path: plan.source })}</div>
        : c.binary ? <div className="why">{t("ext.not_a_text_file_so")}</div>
        : <pre className="skupd-diff">{c.diff ? c.diff.replace(/\n$/, "").split("\n").map((line, i) => <span key={i} data-d={line.startsWith("---") || line.startsWith("+++") ? undefined : line[0]}>{line}{"\n"}</span>) : t("ext.no_difference_to_show")}</pre>}
    </details>
  );

  return (
    <div className="skupd mkt addpkg" data-stage="confirm" aria-busy={busy}>
      <div className="find">
        <span className="t">{plan.changes.length > 0 ? t("ext.update_name_n_change_s", { name: plan.name, n: plan.changes.length }) : record ? t("ext.name_already_matches_its_source", { name: plan.name }) : t("ext.name_matches_its_source_folder", { name: plan.name })}</span>
        <span className="why">{t("ext.source_path_after_the_update", { path: plan.source })}</span>
      </div>
      {plan.overwritesLocal && (
        <div className="find" data-lvl="warn">
          <span className="t">{t("ext.the_installed_copy_was_edited")}</span>
          <span className="why">{t("ext.the_update_makes_the_copy")}</span>
          <label className="mkt-seen"><input type="checkbox" data-action="skills.update-overwrite" checked={sureOverwrite} disabled={busy} onChange={(e) => setSureOverwrite(e.target.checked)} />{t("ext.i_understand_this_overwrites_local")}</label>
        </div>
      )}
      {code.length > 0 && (
        <section className="rgrp" data-lvl="high">
          <h3>{t("ext.these_files_are_not_plain")}</h3>
          {code.map(file)}
          {unread > 0 && <div className="why">{t("ext.n_file_s_not_opened", { n: unread })}</div>}
        </section>
      )}
      {text.length > 0 && (
        <section className="rgrp" data-lvl="low">
          <h3>{t("ext.these_files_are_plain_instructions")}</h3>
          {text.map(file)}
        </section>
      )}
      {hidden.length > 0 && (
        <label className="mkt-seen"><input type="checkbox" data-action="skills.update-hidden" checked={sureTruncated} disabled={busy} onChange={(e) => setSureTruncated(e.target.checked)} />{t("ext.n_file_s_cannot_be", { n: hidden.length })}</label>
      )}
      <div className="acts">
        <span className="note">{t("ext.replaces_the_installed_copy_under")}</span>
        {stale && <button className="act" data-action="skills.update-reload" disabled={busy} onClick={load}>{t("ext.review_again")}</button>}
        <button className="act" data-action="skills.update-close" disabled={busy} onClick={onClose}>{t("common.cancel")}</button>
        <button className="act" data-action="skills.update-apply" data-primary disabled={busy || !ready} onClick={() => void apply()}>{t(busy ? "ext.updating" : "ext.update")}</button>
      </div>
      {error && <div className="why" role="alert">{error}</div>}
    </div>
  );
}
