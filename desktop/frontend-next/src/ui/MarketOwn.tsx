import { useEffect, useRef, useState } from "react";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import type { AgentPort, MarketPackage, MarketPlan } from "../port/port";
import { Outcome } from "./AddPlugin";
import { PlanConfirm } from "./MarketConfirm";

interface Props {
  port: AgentPort;
  pkg: MarketPackage;
  onBack: () => void;
  onInstalled: () => void;
  onApplying?: (applying: boolean) => void;
  onViewInstalled?: (kind: string, name: string) => void;
}

// A publisher installing their own package: the kernel fetches it from the
// registry as the account, previews it, and installs only what matches that
// preview's digest. This view never holds a source of its own to send.
export function OwnInstall({ port, pkg, onBack, onInstalled, onViewInstalled, onApplying }: Props) {
  const [plan, setPlan] = useState<MarketPlan | null>(null);
  const [done, setDone] = useState<MarketPlan | null>(null);
  const [busy, setBusy] = useState(true);
  const [error, setError] = useState("");
  const [attempt, setAttempt] = useState(0);
  const generation = useRef(0);
  const connection = useRef({ port });
  if (connection.current.port !== port) connection.current = { port };
  const replace = !!pkg.installed && pkg.installed.version !== pkg.latestVersion;

  useEffect(() => {
    const current = ++generation.current;
    const live = () => current === generation.current;
    setBusy(true);
    setError("");
    setPlan(null);
    setDone(null);
    onApplying?.(false);
    port
      .planOwnMarket({ slug: pkg.slug, replace })
      .then((p) => {
        if (!live()) return;
        if (!p.ok) setError(p.error || p.next || t("该来源中没有可安装的内容"));
        else setPlan(p);
      })
      .catch((e) => live() && setError(reason(e)))
      .finally(() => live() && setBusy(false));
    return () => {
      ++generation.current;
      onApplying?.(false);
    };
  }, [port, pkg.slug, replace, attempt, onApplying]);

  const install = async () => {
    if (!plan) return;
    const current = generation.current;
    const owner = connection.current;
    setBusy(true);
    onApplying?.(true);
    setError("");
    try {
      const out = await port.installOwnMarket({
        slug: pkg.slug, version: plan.version, planId: plan.planId, replace, digest: plan.contentDigest,
      });
      if (current === generation.current) setDone(out);
      if (out.applied && connection.current === owner) onInstalled();
    } catch (e) {
      if (current === generation.current) setError(reason(e));
    } finally {
      if (current === generation.current) { setBusy(false); onApplying?.(false); }
    }
  };

  const back = (
    <button className="act" data-action="market.back" onClick={onBack}>
      {t("返回我的发布")}
    </button>
  );

  if (done) {
    const installed = done.applied ? done.actions?.filter((action) => action.status === "done" && action.name) ?? [] : [];
    const location = pkg.kind === "plugin" || pkg.kind === "theme"
      ? installed.find((action) => action.kind === "plugin") : installed[0];
    return (
      <div className="mkt addpkg" data-stage="done">
        <Outcome plan={done} />
        {installed.length > 0 && <ul className="mkt-installed">{installed.map((action, i) => <li key={`${action.kind}:${action.name}:${i}`}>{action.name}</li>)}</ul>}
        <div className="acts">
          {back}
          {!done.ok && <button className="act" data-action="market.own-retry" onClick={() => setAttempt((n) => n + 1)}>{t("重试")}</button>}
          {location && onViewInstalled && (
            <button className="act" data-action="market.view-installed" onClick={() => onViewInstalled(location.kind, location.name!)}>{t("查看已安装能力")}</button>
          )}
        </div>
      </div>
    );
  }
  if (plan) {
    return <PlanConfirm key={plan.planId} slug={pkg.slug} plan={plan} busy={busy} error={error} onCancel={onBack} onInstall={() => void install()} own />;
  }
  return (
    <div className="mkt" aria-busy={busy}>
      {error ? (
        <div className="find" data-lvl="err" role="alert">
          <span className="t">{t("无法预览 {name}", { name: pkg.slug })}</span>
          <span className="why">{error}</span>
        </div>
      ) : (
        <div className="empty" role="status">{t("正在预览将安装的内容…")}</div>
      )}
      <div className="acts">
        {back}
        {error && !busy && <button className="act" data-action="market.own-retry" onClick={() => setAttempt((n) => n + 1)}>{t("重试")}</button>}
      </div>
    </div>
  );
}
