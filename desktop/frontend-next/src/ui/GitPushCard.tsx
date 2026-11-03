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
      {t("gitPushCard.push")}
    </button>
  );
  if (state.phase === "idle") return { button, card: null };

  if (state.phase === "reading") {
    return { button, card: (
      <section className="push-card" data-phase="reading" aria-label={t("common.push")}>
        <p className="refine-wait" role="status">{t("gitPushCard.readingWhatWouldBePushed")}</p>
        <footer><button type="button" className="btn sm" data-weak="" data-action="push.close" onClick={close}>{t("common.cancel")}</button></footer>
      </section>
    ) };
  }

  if (state.phase === "failed") {
    return { button, card: (
      <section className="push-card" data-phase="failed" aria-label={t("common.push")}>
        <p className="refine-error" role="alert">{state.error}</p>
        <footer>
          <button type="button" className="btn sm" data-action="push.preview" onClick={preview}>{t("common.readAgain")}</button>
          <button type="button" className="btn sm" data-weak="" data-action="push.close" onClick={close}>{t("common.close")}</button>
        </footer>
      </section>
    ) };
  }

  if (state.phase === "done") {
    const { plan } = state.result;
    return { button, card: (
      <section className="push-card" data-phase="done" aria-label={t("common.push")}>
        <p className="commit-done" role="status">
          {t(state.result.new ? "gitPushCard.pushedToTheBranchIs" : "gitPushCard.pushedCommitsTo", { branch: plan.upstreamBranch, host: plan.host, n: String(plan.ahead) })}
        </p>
        <footer><button type="button" className="btn sm" data-weak="" data-action="push.close" onClick={close}>{t("common.collapse")}</button></footer>
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
    <section className="push-card" data-phase="ready" aria-label={t("common.push")}>
      <header>
        <b>{t("gitPushCard.pushTo", { host: plan.host })}</b>
        <small data-scheme={plan.scheme}>{plan.scheme}</small>
      </header>
      <dl>
        <dt>{t("common.branch")}</dt>
        <dd>{plan.branch} → {plan.remote}/{plan.upstreamBranch}</dd>
        <dt>{t("gitPushCard.remote")}</dt>
        <dd className="push-url" title={plan.url}>{plan.url}</dd>
        <dt>{t("gitPushCard.willSend")}</dt>
        <dd>{t("gitPushCard.commits", { n: String(plan.ahead) })}{plan.head ? ` · ${plan.head.slice(0, 7)}` : ""}</dd>
      </dl>
      {plan.behind > 0 && <p className="push-behind" role="status">{t("gitPushCard.theRemoteHasCommitsThis", { n: String(plan.behind) })}</p>}
      {plan.ahead === 0 && <p className="push-behind" role="status">{t("gitPushCard.theRemoteAlreadyHasEvery")}</p>}
      {error && <p className="refine-error" role="alert">{error}</p>}
      <small>{t("gitPushCard.pushesThisOneBranchAnd")}</small>
      <footer>
        <button type="button" className="btn sm" data-primary data-action="push.confirm" disabled={pushing || plan.ahead === 0} onClick={confirm}>
          {pushing ? t("gitPushCard.pushing") : t("gitPushCard.pushTo", { host: plan.host })}
        </button>
        <button type="button" className="btn sm" data-weak="" data-action="push.close" disabled={pushing} onClick={close}>{t("common.cancel")}</button>
      </footer>
    </section>
  ) };
}
