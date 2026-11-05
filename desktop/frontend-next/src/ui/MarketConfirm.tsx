import { useState } from "react";
import { t } from "../i18n";
import type { MarketPlan } from "../port/port";
import { Candidate, ORDER, PreviewCut } from "./AddPlugin";
import { StudioIcon } from "./StudioIcon";

// Titled for what a person has to do with each group, not for what the kernel
// fears: a high grade here is as often "many skills from one address" as it is
// "a process starts".
const GROUP_TITLE: Record<string, string> = {
  high: "ext.read_each_of_these_before",
  medium: "ext.these_change_what_is_available",
  low: "ext.these_only_add_files",
};

interface Props {
  slug: string;
  plan: MarketPlan;
  busy: boolean;
  error: string;
  onCancel: () => void;
  onInstall: () => void;
  // The person's own package rather than a listed one they chose to trust.
  own?: boolean;
}

// The one confirmation every market install passes: the plan as the kernel
// graded it, with a source that expands into several skills held until read.
// What the pin is — a reviewer's digest or this preview's own — is the plan's
// to say, so an unreviewed install cannot read as a reviewed one.
export function PlanConfirm({ slug, plan, busy, error, onCancel, onInstall, own }: Props) {
  const [seen, setSeen] = useState(false);
  const actions = plan.actions ?? [];
  const skills = actions.filter((a) => a.kind === "skill");
  const skillTotal = Math.max(plan.kinds?.skill ?? 0, skills.length);
  const many = skillTotal > 1;
  const groups = ORDER.map((level) => ({ level, actions: actions.filter((a) => (a.riskLevel || "low") === level) })).filter(
    (g) => g.actions.length > 0,
  );
  return (
    <div className="mkt addpkg mkc" data-stage="confirm" aria-busy={busy}>
      <div className="mkc-head">
        <b>{t("ext.name_version_will_install_the", { name: slug, version: plan.version })}</b>
        <button type="button" className="mkc-x" data-action="market.cancel" aria-label={t("ext.close_install_confirmation")} title={t("ext.close_install_confirmation")} disabled={busy} onClick={onCancel}>
          <StudioIcon name="close" />
        </button>
      </div>
      {plan.unreviewed && (own ? (
        <div className="find" data-lvl="warn" data-unreviewed="">
          <span className="t">{t("ext.unreviewed_visible_only_to_you")}</span>
          <span className="why">{t("ext.this_is_your_own_version")}</span>
        </div>
      ) : (
        <div className="find" data-lvl="warn" data-unreviewed="">
          <span className="t">{t("ext.you_chose_to_trust_this")}</span>
          <span className="why">{t("ext.this_is_what_the_source")}</span>
        </div>
      ))}
      <div className="find mkc-check" data-pinned={plan.unreviewed ? undefined : ""}>
        {!plan.unreviewed && <span className="t"><StudioIcon name="check" />{t("ext.checked_by_content_digest_identical")}</span>}
        {plan.unreviewed && <span className="why">{t("ext.content_digest_digest", { digest: plan.contentDigest ?? "" })}</span>}
        {!plan.unreviewed && plan.contentDigest && <code className="mkc-digest">{plan.contentDigest}</code>}
        {/* An MCP entry pins how a server is started, not what it fetches
            or answers with once running; saying otherwise would be believed. */}
        {actions.some((a) => a.kind === "mcp") && (
          <span className="why">{t("ext.for_an_mcp_server_only")}</span>
        )}
      </div>
      {many && (
        <div className="find mkt-many" data-lvl="warn">
          <span className="t">{t("ext.this_source_holds_n_skills", { n: skillTotal })}</span>
          <ul className="mkt-names">
            {skills.map((a, i) => (
              <li key={`${a.name}:${i}`}>{a.name}</li>
            ))}
            {skillTotal > skills.length && <li>{t("ext.n_more_skills_are_not", { n: skillTotal - skills.length })}</li>}
          </ul>
          <label className="mkt-seen">
            <input type="checkbox" data-action="market.confirm-many" disabled={busy} checked={seen} onChange={(e) => setSeen(e.target.checked)} />
            {t("ext.i_have_read_these_n", { n: skillTotal })}
          </label>
        </div>
      )}
      {groups.map((g) => (
        <section className="rgrp" key={g.level} data-lvl={g.level}>
          <h3>{t(GROUP_TITLE[g.level] ?? g.level)}</h3>
          {g.actions.map((a, i) => (
            <Candidate a={a} key={`${a.kind}:${a.name}:${i}`} />
          ))}
        </section>
      ))}
      <PreviewCut shown={plan.previewTruncated} hidden={plan.hiddenActions} />
      {plan.warnings?.map((w) => (
        <div className="why" key={w}>
          {w}
        </div>
      ))}
      <div className="acts">
        <span className="note">{t("ext.install_to_mine_available_in")}</span>
        <button className="act" data-action="market.cancel" disabled={busy} onClick={onCancel}>
          {t("common.back")}
        </button>
        <button className="act" data-action="market.install" data-primary disabled={busy || (many && !seen)} onClick={onInstall}>
          {t(busy ? "ext.installing" : "ext.install")}
        </button>
      </div>
      {error && <div className="why" role="alert">{error}</div>}
    </div>
  );
}
