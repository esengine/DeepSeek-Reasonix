import { useEffect, useRef, useState } from "react";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import type { AgentPort, PushPlan, PushResult } from "../port/port";

type Phase =
  | { phase: "idle" }
  | { phase: "reading" }
  | { phase: "ready"; plan: PushPlan; pushing: boolean; error?: string }
  | { phase: "done"; result: PushResult }
  | { phase: "failed"; error: string };

// The one git action that leaves the machine, so it is always two steps: what
// would go and where is read first, with no network, and only the confirm button
// sends exactly that plan. It is never forced and never automatic.
export function usePushCard(port: AgentPort, repo: string | undefined, onPushed: () => void) {
  const [state, setState] = useState<Phase>({ phase: "idle" });
  const live = useRef(true);
  useEffect(() => () => { live.current = false; }, []);

  const preview = () => {
    setState({ phase: "reading" });
    port.pushPreview(repo).then(
      (plan) => live.current && setState({ phase: "ready", plan, pushing: false }),
      (e) => live.current && setState({ phase: "failed", error: reason(e) }),
    );
  };
  const close = () => setState({ phase: "idle" });

  const button = (
    <button type="button" className="btn sm push-open" data-action="push.preview" disabled={state.phase !== "idle"} onClick={preview}>
      {t("推送…")}
    </button>
  );
  if (state.phase === "idle") return { button, card: null };

  if (state.phase === "reading") {
    return { button, card: (
      <section className="push-card" data-phase="reading" aria-label={t("推送")}>
        <p className="refine-wait" role="status">{t("读取要推送的内容…")}</p>
        <footer><button type="button" className="btn sm" data-weak="" data-action="push.close" onClick={close}>{t("取消")}</button></footer>
      </section>
    ) };
  }

  if (state.phase === "failed") {
    return { button, card: (
      <section className="push-card" data-phase="failed" aria-label={t("推送")}>
        <p className="refine-error" role="alert">{state.error}</p>
        <footer>
          <button type="button" className="btn sm" data-action="push.preview" onClick={preview}>{t("重新读取")}</button>
          <button type="button" className="btn sm" data-weak="" data-action="push.close" onClick={close}>{t("关闭")}</button>
        </footer>
      </section>
    ) };
  }

  if (state.phase === "done") {
    const { plan } = state.result;
    return { button, card: (
      <section className="push-card" data-phase="done" aria-label={t("推送")}>
        <p className="commit-done" role="status">
          {t(state.result.new ? "已把 {branch} 推送到 {host}，远端新建了这个分支" : "已把 {n} 个提交推送到 {host}", { branch: plan.upstreamBranch, host: plan.host, n: String(plan.ahead) })}
        </p>
        {state.result.trackingStatus === "not_updated" && <p role="status">{t("远端推送成功，但本地跟踪引用未更新。请刷新仓库状态，无需重复推送。")}</p>}
        <footer><button type="button" className="btn sm" data-weak="" data-action="push.close" onClick={close}>{t("收起")}</button></footer>
      </section>
    ) };
  }

  const { plan, pushing, error } = state;
  const confirm = () => {
    setState({ ...state, pushing: true, error: undefined });
    port.push(repo ? { repo, plan } : { plan }).then(
      (result) => {
        if (!live.current) return;
        setState({ phase: "done", result });
        onPushed();
      },
      (e) => live.current && setState({ phase: "ready", plan, pushing: false, error: reason(e) }),
    );
  };
  return { button, card: (
    <section className="push-card" data-phase="ready" aria-label={t("推送")}>
      <header>
        <b>{t("推送到 {host}", { host: plan.host })}</b>
        <small data-scheme={plan.scheme}>{plan.scheme}</small>
      </header>
      <dl>
        <dt>{t("分支")}</dt>
        <dd>{plan.branch} → {plan.remote}/{plan.upstreamBranch}</dd>
        <dt>{t("远端")}</dt>
        <dd className="push-url" title={plan.url}>{plan.url}</dd>
        <dt>{t("将发送")}</dt>
        <dd>{t("{n} 个提交", { n: String(plan.ahead) })}{plan.head ? ` · ${plan.head.slice(0, 7)}` : ""}</dd>
      </dl>
      {plan.behind > 0 && <p className="push-behind" role="status">{t("远端比这个分支多 {n} 个提交（以上次获取为准），推送可能被拒绝。", { n: String(plan.behind) })}</p>}
      {plan.ahead === 0 && <p className="push-behind" role="status">{t("远端已经有这个分支的全部提交，没有可推送的内容。")}</p>}
      {error && <p className="refine-error" role="alert">{error}</p>}
      <small>{t("只推送这一个分支，从不强制推送；仓库里的钩子（包括 pre-push）不会运行。")}</small>
      <footer>
        <button type="button" className="btn sm" data-primary data-action="push.confirm" disabled={pushing || plan.ahead === 0} onClick={confirm}>
          {pushing ? t("正在推送…") : t("推送到 {host}", { host: plan.host })}
        </button>
        <button type="button" className="btn sm" data-weak="" data-action="push.close" disabled={pushing} onClick={close}>{t("取消")}</button>
      </footer>
    </section>
  ) };
}
