import { useState } from "react";
import { t } from "../i18n";
import type { MarketPlan } from "../port/port";
import { Candidate, ORDER, PreviewCut } from "./AddPlugin";

// Titled for what a person has to do with each group, not for what the kernel
// fears: a high grade here is as often "many skills from one address" as it is
// "a process starts".
const GROUP_TITLE: Record<string, string> = {
  high: "marketConfirm.readEachOfTheseBefore",
  medium: "common.theseChangeWhatIsAvailable",
  low: "common.theseOnlyAddFiles",
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
    <div className="mkt addpkg" data-stage="confirm" aria-busy={busy}>
      {plan.unreviewed && (own ? (
        <div className="find" data-lvl="warn" data-unreviewed="">
          <span className="t">{t("marketConfirm.unreviewedVisibleOnlyToYou")}</span>
          <span className="why">{t("marketConfirm.thisIsYourOwnVersion")}</span>
        </div>
      ) : (
        <div className="find" data-lvl="warn" data-unreviewed="">
          <span className="t">{t("marketConfirm.youChoseToTrustThis")}</span>
          <span className="why">{t("marketConfirm.thisIsWhatTheSource")}</span>
        </div>
      ))}
      <div className="find">
        <span className="t">{t("marketConfirm.willInstallTheFollowing", { name: slug, version: plan.version })}</span>
        <span className="why">
          {plan.unreviewed ? t("marketConfirm.contentDigest", { digest: plan.contentDigest ?? "" }) : t("marketConfirm.checkedByContentDigestIdentical")}
        </span>
        {/* An MCP entry pins how a server is started, not what it fetches
            or answers with once running; saying otherwise would be believed. */}
        {actions.some((a) => a.kind === "mcp") && (
          <span className="why">{t("marketConfirm.forAnMCPServerOnly")}</span>
        )}
      </div>
      {many && (
        <div className="find mkt-many" data-lvl="warn">
          <span className="t">{t("marketConfirm.thisSourceHoldsSkillsAnd", { n: skillTotal })}</span>
          <ul className="mkt-names">
            {skills.map((a, i) => (
              <li key={`${a.name}:${i}`}>{a.name}</li>
            ))}
            {skillTotal > skills.length && <li>{t("marketConfirm.moreSkillsAreNotShown", { n: skillTotal - skills.length })}</li>}
          </ul>
          <label className="mkt-seen">
            <input type="checkbox" data-action="market.confirm-many" disabled={busy} checked={seen} onChange={(e) => setSeen(e.target.checked)} />
            {t("marketConfirm.iHaveReadTheseSkills", { n: skillTotal })}
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
        <span className="note">{t("common.installToMineAvailableIn")}</span>
        <button className="act" data-action="market.cancel" disabled={busy} onClick={onCancel}>
          {t("common.back")}
        </button>
        <button className="act" data-action="market.install" data-primary disabled={busy || (many && !seen)} onClick={onInstall}>
          {t(busy ? "common.installing" : "common.install")}
        </button>
      </div>
      {error && <div className="why" role="alert">{error}</div>}
    </div>
  );
}
