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
      {t("workbench.push_2")}
    </button>
  );
  if (state.phase === "idle") return { button, card: null };

  if (state.phase === "reading") {
    return { button, card: (
      <section className="push-card" data-phase="reading" aria-label={t("workbench.push")}>
        <p className="refine-wait" role="status">{t("workbench.reading_what_would_be_pushed")}</p>
        <footer><button type="button" className="btn sm" data-weak="" data-action="push.close" onClick={close}>{t("common.cancel")}</button></footer>
      </section>
    ) };
  }

  if (state.phase === "failed") {
    return { button, card: (
      <section className="push-card" data-phase="failed" aria-label={t("workbench.push")}>
        <p className="refine-error" role="alert">{state.error}</p>
        <footer>
          <button type="button" className="btn sm" data-action="push.preview" onClick={preview}>{t("common.read_again")}</button>
          <button type="button" className="btn sm" data-weak="" data-action="push.close" onClick={close}>{t("common.close")}</button>
        </footer>
      </section>
    ) };
  }

  if (state.phase === "done") {
    const { plan } = state.result;
    return { button, card: (
      <section className="push-card" data-phase="done" aria-label={t("workbench.push")}>
        <p className="commit-done" role="status">
          {t(state.result.new ? "workbench.pushed_branch_to_host_the" : "workbench.pushed_n_commits_to_host", { branch: plan.upstreamBranch, host: plan.host, n: String(plan.ahead) })}
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
    <section className="push-card" data-phase="ready" aria-label={t("workbench.push")}>
      <header>
        <b>{t("workbench.push_to_host", { host: plan.host })}</b>
        <small data-scheme={plan.scheme}>{plan.scheme}</small>
      </header>
      <dl>
        <dt>{t("common.branch")}</dt>
        <dd>{plan.branch} → {plan.remote}/{plan.upstreamBranch}</dd>
        <dt>{t("workbench.remote")}</dt>
        <dd className="push-url" title={plan.url}>{plan.url}</dd>
        <dt>{t("workbench.will_send")}</dt>
        <dd>{t("workbench.n_commits", { n: String(plan.ahead) })}{plan.head ? ` · ${plan.head.slice(0, 7)}` : ""}</dd>
      </dl>
      {plan.behind > 0 && <p className="push-behind" role="status">{t("workbench.the_remote_has_n_commits", { n: String(plan.behind) })}</p>}
      {plan.ahead === 0 && <p className="push-behind" role="status">{t("workbench.the_remote_already_has_every")}</p>}
      {error && <p className="refine-error" role="alert">{error}</p>}
      <small>{t("workbench.pushes_this_one_branch_and")}</small>
      <footer>
        <button type="button" className="btn sm" data-primary data-action="push.confirm" disabled={pushing || plan.ahead === 0} onClick={confirm}>
          {pushing ? t("workbench.pushing") : t("workbench.push_to_host", { host: plan.host })}
        </button>
        <button type="button" className="btn sm" data-weak="" data-action="push.close" disabled={pushing} onClick={close}>{t("common.cancel")}</button>
      </footer>
    </section>
  ) };
}
