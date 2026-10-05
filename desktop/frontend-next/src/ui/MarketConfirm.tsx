import { useState } from "react";
import { t } from "../i18n";
import type { MarketPlan } from "../port/port";
import { Candidate, ORDER } from "./AddPlugin";

// Titled for what a person has to do with each group, not for what the kernel
// fears: a high grade here is as often "many skills from one address" as it is
// "a process starts".
const GROUP_TITLE: Record<string, string> = {
  high: "以下项目需要逐项看过再安装",
  medium: "以下项目会改变可用能力",
  low: "以下项目仅添加文件",
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
  const many = skills.length > 1;
  const groups = ORDER.map((level) => ({ level, actions: actions.filter((a) => (a.riskLevel || "low") === level) })).filter(
    (g) => g.actions.length > 0,
  );
  return (
    <div className="mkt addpkg" data-stage="confirm" aria-busy={busy}>
      {plan.unreviewed && (own ? (
        <div className="find" data-lvl="warn" data-unreviewed="">
          <span className="t">{t("未审核 · 仅你可见")}</span>
          <span className="why">{t("这是你自己发布、还没有通过审核的版本。安装会核对与这次预览相同的内容摘要，内容在确认后有变化就拒绝安装。")}</span>
        </div>
      ) : (
        <div className="find" data-lvl="warn" data-unreviewed="">
          <span className="t">{t("你选择了信任这个发布者")}</span>
          <span className="why">{t("以下是来源现在提供的内容，可能与审核时不同。确认后内容若有变化，安装会被拒绝。")}</span>
        </div>
      ))}
      <div className="find">
        <span className="t">{t("{name} {version} 将安装以下内容", { name: slug, version: plan.version })}</span>
        <span className="why">
          {plan.unreviewed ? t("内容摘要：{digest}", { digest: plan.contentDigest ?? "" }) : t("已按内容摘要核对：与审核时固定的版本一致。")}
        </span>
        {/* An MCP entry pins how a server is started, not what it fetches
            or answers with once running; saying otherwise would be believed. */}
        {actions.some((a) => a.kind === "mcp") && (
          <span className="why">{t("MCP 服务只固定了启动方式与连接地址；它启动后自行下载或远端提供的代码不在固定范围内。")}</span>
        )}
      </div>
      {many && (
        <div className="find mkt-many" data-lvl="warn">
          <span className="t">{t("这个来源包含 {n} 个技能，会全部安装", { n: skills.length })}</span>
          <ul className="mkt-names">
            {skills.map((a, i) => (
              <li key={`${a.name}:${i}`}>{a.name}</li>
            ))}
          </ul>
          <label className="mkt-seen">
            <input type="checkbox" data-action="market.confirm-many" disabled={busy} checked={seen} onChange={(e) => setSeen(e.target.checked)} />
            {t("我已看过这 {n} 个技能，全部安装", { n: skills.length })}
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
      {plan.warnings?.map((w) => (
        <div className="why" key={w}>
          {w}
        </div>
      ))}
      <div className="acts">
        <span className="note">{t("安装到「我的」，所有项目均可使用")}</span>
        <button className="act" data-action="market.cancel" disabled={busy} onClick={onCancel}>
          {t("返回")}
        </button>
        <button className="act" data-action="market.install" data-primary disabled={busy || (many && !seen)} onClick={onInstall}>
          {t(busy ? "安装中…" : "安装")}
        </button>
      </div>
      {error && <div className="why" role="alert">{error}</div>}
    </div>
  );
}
