import { CloudOff, Loader2, RotateCw, TriangleAlert } from "lucide-react";
import { useEffect, useState } from "react";
import { useT } from "../lib/i18n";
import { app } from "../lib/bridge";
import { Transcript } from "./Transcript";
import { AskCard } from "./AskCard";
import { ApprovalModal } from "./ApprovalModal";
import { ExtensionFormDialog } from "./ExtensionFormDialog";
import type { RemoteSessionApi } from "../lib/useRemoteSession";
export { hydrateRemoteTelemetry, loadRemoteStatusSnapshot } from "../lib/remoteTelemetry";
import type { TabMeta, WireApproval, WireAsk } from "../lib/types";

/**
 * RemoteSessionSurface renders the active remote tab's content area with
 * the SAME Transcript component local tabs use — the session hook feeds the
 * shared reducer with serve frames, so items, live streaming, approvals, and
 * asks arrive in the local shapes. Only the connection state machine and
 * the approval/ask cards are remote-specific; the composer lives in the
 * app shell, shared with local tabs.
 */
export function RemoteSessionSurface({ tab, session }: { tab: TabMeta; session: RemoteSessionApi }) {
  const t = useT();
  const approval = session.transcript.approval as WireApproval | undefined;
  const ask = session.transcript.ask as WireAsk | undefined;
  const extensionForm = session.transcript.extensionForm;
  const [actionError, setActionError] = useState("");
  const [extensionFormBusy, setExtensionFormBusy] = useState(false);
  useEffect(() => { setActionError(""); setExtensionFormBusy(false); }, [session.state, tab.id]);
  const runAction = (action: () => Promise<unknown>) => {
    setActionError("");
    void action().catch((error) => setActionError(error instanceof Error ? error.message : String(error)));
  };
  const submitExtensionForm = (values: Record<string, unknown>) => {
    if (!extensionForm || extensionFormBusy) return;
    setExtensionFormBusy(true);
    runAction(() => app.SubmitRemoteTabExtensionForm(tab.id, extensionForm.pluginId, extensionForm.surfaceId, values)
      .then(() => session.clearExtensionForm(extensionForm.pluginId, extensionForm.surfaceId))
      .finally(() => setExtensionFormBusy(false)));
  };
  if (!tab.remote) return null;

  if (session.state === "disconnected") {
    // A restored shell: reconnect lands in a fresh blank session; past
    // conversations are picked from the tree group's session rows.
    const reconnect = () => {
      runAction(() => app.OpenRemoteProjectTab(tab.remote!.hostId, tab.remote!.workspace, { newSession: true }));
    };
    return (
      <div className="remote-surface remote-surface--disconnected" role="status">
        <CloudOff size={18} aria-hidden="true" />
        <span>{t("remoteSurface.disconnected")}</span>
        <span className="remote-surface__detail">{t("remoteSurface.disconnectedHint")}</span>
        {actionError ? <span className="remote-surface__detail" role="alert">{actionError}</span> : null}
        <button type="button" className="btn btn--ghost" onClick={reconnect}>
          <RotateCw size={14} aria-hidden="true" />
          {t("remoteSurface.reconnect")}
        </button>
      </div>
    );
  }

  if (session.state === "serve_down") {
    const retry = () => {
      // With no explicit target, the backend preserves the parked tab's
      // current named/fresh-session intent instead of silently starting over.
      runAction(() => app.OpenRemoteProjectTab(tab.remote!.hostId, tab.remote!.workspace, {}));
    };
    return (
      <div className="remote-surface remote-surface--warning" role="alert">
        <TriangleAlert size={18} aria-hidden="true" />
        <span>{t("remoteSurface.serveDown")}</span>
        {actionError || session.error ? <span className="remote-surface__detail">{actionError || session.error}</span> : null}
        <button type="button" className="btn btn--ghost" onClick={retry}>
          <RotateCw size={14} aria-hidden="true" />
          {t("remoteSurface.reconnect")}
        </button>
      </div>
    );
  }

  if (session.state === "error") {
    return (
      <div className="remote-surface remote-surface--error" role="alert">
        <CloudOff size={18} aria-hidden="true" />
        <span>{t("remoteSurface.error")}</span>
        {session.error ? <span className="remote-surface__detail">{session.error}</span> : null}
      </div>
    );
  }

  if (!session.hydrated && session.error) {
    return (
      <div className="remote-surface remote-surface--error" role="alert">
        <TriangleAlert size={18} aria-hidden="true" />
        <span>{t("remoteSurface.error")}</span>
        <span className="remote-surface__detail">{session.error}</span>
        <button type="button" className="btn btn--ghost" onClick={() => runAction(session.retryHydration)}>
          <RotateCw size={14} aria-hidden="true" />
          {t("common.retry")}
        </button>
      </div>
    );
  }

  if (!session.hydrated && (session.state === "connecting" || session.state === "reconnecting")) {
    return (
      <div className="remote-surface remote-surface--waiting" role="status">
        <Loader2 size={18} className="remote-surface__spinner" aria-hidden="true" />
        <span>{t(session.state === "connecting" ? "remoteSurface.connecting" : "remoteSurface.reconnecting")}</span>
      </div>
    );
  }

  return (
    <div className="remote-surface remote-surface--ready">
      <Transcript
        items={session.transcript.items}
        live={session.transcript.live}
        tabId={tab.id}
        revealSignal={session.surfaceGeneration}
        running={session.transcript.running}
        checkpoints={session.transcript.checkpoints}
        onPrompt={(prompt) => runAction(() => session.submit(prompt))}
        onRewind={(turn, scope) => runAction(() => session.rewind(turn, scope))}
        rewindDisabled={session.running || !session.hydrated}
      />

      {approval ? (
        <div className="remote-surface__approval">
          <ApprovalModal
            key={`${tab.id}:${approval.id}`}
            approval={approval}
            cwd={tab.cwd}
            tabId={tab.id}
            toolApprovalMode={session.composerProfile?.toolApprovalMode}
            onAnswer={(allow, sessionScope, persist) => runAction(() => approval.tool === "exit_plan_mode"
              ? session.resolvePlanDecision(approval.id, allow ? "start_execution" : "revise_plan")
              : session.approve(
                  approval.id,
                  allow ? (persist ? "persist" : sessionScope ? "session" : "allow") : "deny",
                ))}
            onRevisePlan={(text) => runAction(() => session.resolvePlanDecision(approval.id, "revise_plan", text))}
            onExitPlan={() => runAction(() => session.resolvePlanDecision(approval.id, "exit_plan"))}
            onStop={() => runAction(session.cancelTurn)}
          />
        </div>
      ) : null}

      {ask?.questions?.length ? (
        <AskCard
          key={`${tab.id}:${ask.id}`}
          ask={ask}
          onAnswer={(id, answers) => runAction(() => session.answer(id, answers.map((answer) => ({
            QuestionID: answer.questionId,
            Selected: answer.selected,
          }))))}
          onDismiss={() => runAction(() => session.answer(ask.id, []))}
          onStop={() => runAction(() => session.cancelTurn())}
        />
      ) : null}
      {extensionForm ? (
        <ExtensionFormDialog
          key={`${tab.id}:${extensionForm.pluginId}:${extensionForm.surfaceId}`}
          surface={extensionForm}
          busy={extensionFormBusy}
          onSubmit={submitExtensionForm}
          onCancel={() => submitExtensionForm({ cancelled: true })}
        />
      ) : null}
      {actionError || session.promptError || (session.state === "ready" && session.error) ? (
        <div className="remote-surface__detail" role="alert">{actionError || session.promptError || session.error}</div>
      ) : null}
    </div>
  );
}
