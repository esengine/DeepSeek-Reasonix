import { useEffect, useRef, useState } from "react";
import { app, openExternal } from "../lib/bridge";
import { useRemoteNavigationCommand } from "../lib/remoteNavigationCommands";
import { Transcript, type TranscriptProps } from "./Transcript";
import { AskCard } from "./AskCard";
import { ApprovalModal } from "./ApprovalModal";
import { ExtensionFormDialog } from "./ExtensionFormDialog";
import { MCPInteractionCard } from "./MCPInteractionCard";
import { SessionRecoveryBanner, SessionRecoveryPlaceholder } from "./SessionRecoveryBanner";
import { projectSessionAvailability } from "../lib/sessionAvailability";
import type { RemoteSessionApi } from "../lib/useRemoteSession";
export { hydrateRemoteTelemetry, loadRemoteStatusSnapshot } from "../lib/remoteTelemetry";
import type { PromptKind, TabMeta, WireApproval, WireAsk, WireMCPInteraction } from "../lib/types";
import { orderedLocalSubmissions } from "../lib/localSubmissionState";
import { hasSessionGeneration } from "../lib/sessionIdentity";

/**
 * RemoteSessionSurface renders the active remote tab's content area with
 * the SAME Transcript component local tabs use — the session hook feeds the
 * shared reducer with serve frames, so items, live streaming, approvals, and
 * asks arrive in the local shapes. Only the connection state machine and
 * the approval/ask cards are remote-specific; the composer lives in the
 * app shell, shared with local tabs.
 */
export function RemoteSessionSurface({ tab, session, surfaceCommitToken, onSurfacePaintReady }: {
  tab: TabMeta; session: RemoteSessionApi;
} & Pick<TranscriptProps, "surfaceCommitToken" | "onSurfacePaintReady">) {
  const navigateRemote = useRemoteNavigationCommand();
  const availability = projectSessionAvailability({ remote: session });
  const ready = availability.kind === "ready";
  const localSubmissions = orderedLocalSubmissions(session.transcript);
  const hasContent = session.transcript.items.length > 0 || localSubmissions.length > 0 || Boolean(session.transcript.live?.text || session.transcript.live?.reasoning);
  const approval = session.transcript.approval as WireApproval | undefined;
  const ask = session.transcript.ask as WireAsk | undefined;
  const mcpInteraction = session.transcript.mcpInteraction as WireMCPInteraction | undefined;
  const extensionForm = session.transcript.extensionForm;
  const [actionError, setActionError] = useState("");
  const formKey = extensionForm ? `${tab.id}:${extensionForm.pluginId}:${extensionForm.surfaceId}:${extensionForm.formInstanceId}` : "";
  const visibleFormKeyRef = useRef(formKey);
  visibleFormKeyRef.current = formKey;
  const [busyFormKey, setBusyFormKey] = useState("");
  const extensionFormBusy = Boolean(formKey && busyFormKey === formKey);
  const visiblePrompt = approval || ask || mcpInteraction;
  const promptUpgradeRequired = Boolean(visiblePrompt && !tab.interactionTargetSupported);
  const promptIdentityUnavailable = Boolean(visiblePrompt && tab.interactionTargetSupported && (
    !tab.remote?.hostId || !tab.sessionId || !hasSessionGeneration(tab.sessionGeneration) || !visiblePrompt.turnId || !visiblePrompt.runtimeEpoch
  ));
  const promptActionDisabled = promptUpgradeRequired || promptIdentityUnavailable;
  const formUpgradeRequired = Boolean(extensionForm && !tab.extensionFormInstanceSupported);
  const formIdentityUnavailable = Boolean(extensionForm && tab.extensionFormInstanceSupported && (
    !tab.remote?.hostId || !(extensionForm.sessionId ?? tab.sessionId) || !hasSessionGeneration(tab.sessionGeneration) ||
    !extensionForm.generation || !extensionForm.formInstanceExact || !extensionForm.formInstanceId
  ));
  const formActionDisabled = formUpgradeRequired || formIdentityUnavailable;
  const capabilityError = promptUpgradeRequired
    ? "This remote Reasonix Serve must be upgraded before this card can be answered safely."
    : promptIdentityUnavailable
      ? "This card's exact request identity is unavailable. Refresh the session before answering."
    : formUpgradeRequired
      ? "This remote Reasonix Serve must be upgraded before this form can be submitted safely."
      : formIdentityUnavailable
        ? "This form's exact publication identity is unavailable. Refresh the session before submitting."
      : "";
  useEffect(() => { setActionError(""); setBusyFormKey(""); }, [session.state, session.surfaceGeneration, tab.id]);
  const runAction = async (action: () => Promise<unknown>, propagate = false): Promise<void> => {
    setActionError("");
    try {
      await action();
    } catch (error) {
      setActionError(error instanceof Error ? error.message : String(error));
      if (propagate) throw error;
    }
  };
  const exactPromptTarget = (prompt: { id: string; turnId?: string; runtimeEpoch?: string }, kind: PromptKind) => ({
    tabId: tab.id,
    hostId: tab.remote?.hostId ?? "",
    sessionId: tab.sessionId ?? "",
    sessionGeneration: tab.sessionGeneration ?? 0,
    promptId: prompt.id,
    turnId: prompt.turnId ?? "",
    runtimeEpoch: prompt.runtimeEpoch ?? "",
    kind,
  });
  const resolvePrompt = (prompt: { id: string; turnId?: string; runtimeEpoch?: string }, kind: PromptKind, answer: Record<string, unknown>) => {
    if (!tab.interactionTargetSupported || !app.ResolveRemoteTabPromptExact) {
      return Promise.reject(new Error("This remote Reasonix Serve must be upgraded before this card can be answered safely."));
    }
    return app.ResolveRemoteTabPromptExact(exactPromptTarget(prompt, kind), answer);
  };
  const submitExtensionForm = (values: Record<string, unknown>) => {
    if (!extensionForm || extensionFormBusy) return;
    const pending = extensionForm;
    const requestKey = formKey;
    setBusyFormKey(requestKey);
    runAction(async () => {
      if (!tab.extensionFormInstanceSupported || !app.SubmitRemoteTabExtensionFormExact) {
        throw new Error("This remote Reasonix Serve must be upgraded before this form can be submitted safely.");
      }
      await app.SubmitRemoteTabExtensionFormExact({
        tabId: tab.id,
        hostId: tab.remote!.hostId,
        sessionId: pending.sessionId ?? tab.sessionId ?? "",
        sessionGeneration: tab.sessionGeneration ?? 0,
        pluginId: pending.pluginId,
        surfaceId: pending.surfaceId,
        pluginGeneration: pending.generation ?? 0,
        formInstanceId: pending.formInstanceId,
      }, values);
      session.clearExtensionForm(pending.pluginId, pending.surfaceId, pending.formInstanceId);
    }).finally(() => setBusyFormKey((current) => current === requestKey ? "" : current));
  };
  if (!tab.remote) return null;

  return (
    <>
    <SessionRecoveryBanner key={`${tab.id}:${session.surfaceGeneration}`} availability={availability} onRetry={async () => {
      if (availability.source === "history") { await session.retryHydration(); return; }
      // No new-session target: preserve the parked session when reconnecting.
      const outcome = await navigateRemote(tab.remote!, {});
      if (outcome.status === "failed") throw outcome.error;
    }} />
    <main className="main">
    <div className="remote-surface remote-surface--ready">
      {!ready && !hasContent ? <SessionRecoveryPlaceholder availability={availability} /> : <Transcript
        items={session.transcript.items}
        localSubmissions={localSubmissions}
        localSubmissionSendRevision={session.transcript.localSubmissionSendRevision}
        visibleSubmissionHandoffs={session.transcript.visibleSubmissionHandoffs}
        live={session.transcript.live}
        liveStore={session.liveStore}
        tabId={tab.id}
        hostId={tab.remote.hostId}
        geometrySessionKey={`${tab.id}:${session.surfaceGeneration}`}
        hydrating={!session.hydrated && !hasContent}
        surfaceCommitToken={surfaceCommitToken}
        onSurfacePaintReady={onSurfacePaintReady}
        running={session.transcript.running}
        hasOlderHistory={session.transcript.historyHasOlder}
        hasNewerHistory={session.transcript.historyHasNewer}
        loadingNewerHistory={session.transcript.historyNewerLoading}
        newerHistoryError={session.transcript.historyNewerError}
        historyStartTurn={session.transcript.historyStartTurn}
        totalTurns={session.transcript.historyTotalTurns}
        loadingOlderHistory={session.transcript.historyOlderLoading}
        olderHistoryError={session.transcript.historyOlderError}
        onLoadOlderHistory={session.loadOlderHistory}
        onLoadNewerHistory={session.loadNewerHistory}
        onPrompt={(display, submit = display) => runAction(() => session.submit(submit, display))}
        forkTargets={session.transcript.forkTargets}
        // The tab's advertised capability, not the target list, decides whether
        // this serve can create a child at all: an empty list on a capable serve
        // means no completed turn here, which its own reason explains.
        forkBlocked={tab.forkTargetsSupported ? null : "unsupported"}
        onFork={tab.forkTargetsSupported ? (target) => runAction(async () => {
          const child = await session.forkTurn(target);
          // The child session belongs to the serve, so its surface is opened
          // here rather than adopted from a returned desktop tab. Desktop keeps
          // the operation until navigation succeeds, allowing a later click to
          // recover the same child after an unknown result.
          if (!child) return;
          const opened = await navigateRemote(tab.remote!, { sessionId: child.sessionId });
          if (opened.status === "completed") await session.acknowledgeFork(child.operationId);
        }) : undefined}
      />}

      {ready && approval ? (
        <fieldset disabled={promptActionDisabled} style={{ display: "contents" }}>
        <div className="remote-surface__approval">
          <ApprovalModal
            key={`${tab.id}:${tab.sessionGeneration ?? 0}:${approval.runtimeEpoch ?? ""}:${approval.turnId ?? ""}:${approval.id}`}
            approval={approval}
            cwd={tab.cwd}
            tabId={tab.id}
            toolApprovalMode={session.composerProfile?.toolApprovalMode}
            onAnswer={(allow, sessionScope, persist) => runAction(() => approval.tool === "exit_plan_mode"
              ? resolvePrompt(approval, "plan", { action: allow ? "start_execution" : "revise_plan" })
              : resolvePrompt(approval, approval.kind === "recovery" ? "recovery" : "approval", {
                  allow, session: sessionScope, persist, generation: approval.generation, permissionRevision: approval.permissionRevision,
                }), true)}
            onRevisePlan={(text) => runAction(() => resolvePrompt(approval, "plan", { action: "revise_plan", feedback: text }), true)}
            onExitPlan={() => runAction(() => resolvePrompt(approval, "plan", { action: "exit_plan" }), true)}
            onStop={() => runAction(session.cancelTurn, true)}
          />
        </div>
        </fieldset>
      ) : null}

      {ready && ask?.questions?.length ? (
        <fieldset disabled={promptActionDisabled} style={{ display: "contents" }}>
        <AskCard
          key={`${tab.id}:${tab.sessionGeneration ?? 0}:${ask.runtimeEpoch ?? ""}:${ask.turnId ?? ""}:${ask.id}`}
          ask={ask}
          draftScope={JSON.stringify([tab.remote.hostId, tab.sessionId ?? "", tab.sessionGeneration ?? 0, ask.runtimeEpoch ?? "", ask.turnId ?? "", ask.id])}
          onAnswer={(_id, answers) => runAction(() => resolvePrompt(ask, "ask", { questions: answers }), true)}
          onDismiss={() => runAction(() => resolvePrompt(ask, "ask", { questions: [] }), true)}
          onStop={() => runAction(() => session.cancelTurn())}
        />
        </fieldset>
      ) : null}
      {ready && mcpInteraction ? (
        <MCPInteractionCard
          key={`${tab.id}:${tab.sessionGeneration ?? 0}:${mcpInteraction.runtimeEpoch ?? ""}:${mcpInteraction.turnId ?? ""}:${mcpInteraction.id}`}
          instanceKey={JSON.stringify([tab.remote.hostId, tab.sessionId ?? "", tab.sessionGeneration ?? 0, mcpInteraction.runtimeEpoch ?? "", mcpInteraction.turnId ?? "", "mcp", mcpInteraction.id])}
          interaction={mcpInteraction}
          busy={promptActionDisabled}
          onAnswer={(_id, action, content) => void runAction(() => resolvePrompt(mcpInteraction, "mcp", { action, content: content ?? null }))}
          onOpenLink={openExternal}
        />
      ) : null}
      {ready && extensionForm ? (
        <ExtensionFormDialog
          key={formKey}
          surface={extensionForm}
          busy={extensionFormBusy || formActionDisabled}
          onSubmit={submitExtensionForm}
          onCancel={() => submitExtensionForm({ cancelled: true })}
        />
      ) : null}
      {ready && (capabilityError || actionError || session.promptError || session.error) ? (
        <div className="remote-surface__detail" role="alert">{capabilityError || actionError || session.promptError || session.error}</div>
      ) : null}
    </div>
    </main>
    </>
  );
}
