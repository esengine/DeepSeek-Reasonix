import { useEffect, useRef, useState } from "react";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import { HttpError } from "../port/port";
import type { WorktreePreparation } from "../port/workspace";
import { StudioIcon } from "./StudioIcon";
import { CopyButton } from "./CopyButton";
import { WorkspacePath, compactWorkspaceValue, workspaceName } from "./WorkspacePath";
import "../styles/worktree-setup.css";

const phases: Record<string, string> = {
  starting: "worktreeSetupPage.submittingCreationRequest", validating: "worktreeSetupPage.checkingSourceBranch", creating: "worktreeSetupPage.creatingWorkspace",
  checking_out: "worktreeSetupPage.checkingOutCommittedFiles", copying_attachments: "worktreeSetupPage.copyingTaskAttachments", binding: "worktreeSetupPage.bindingWorkspace",
  cleaning: "worktreeSetupPage.cleaningUpIncompleteWorkspace", cancelling: "worktreeSetupPage.stoppingAndCleaningUp", ready: "worktreeSetupPage.workspaceBound",
  cleanup_failed: "worktreeSetupPage.cleanupDidNotFinish", failed: "worktreeSetupPage.creationDidNotFinish", cancelled: "worktreeSetupPage.creationStoppedAndWorkspaceCleaned",
  retained: "worktreeSetupPage.workspaceBound", unreachable: "worktreeSetupPage.waitingForConfirmation",
};

interface Props {
  stage: WorktreePreparation;
  task: string;
  files: string[];
  busy: boolean;
  onRetry: () => void;
  onCancel: () => void;
  onEdit: () => void;
}

export function WorktreeSetupPage({ stage, task, files, busy, onRetry, onCancel, onEdit }: Props) {
  const [now, setNow] = useState(Date.now);
  const [details, setDetails] = useState(false);
  const heading = useRef<HTMLHeadingElement>(null);
  useEffect(() => { heading.current?.focus(); }, [stage.id]);
  useEffect(() => { if (document.activeElement === document.body) heading.current?.focus(); }, [stage.phase]);
  const failed = stage.phase === "failed";
  const cancelled = stage.phase === "cancelled";
  const retained = stage.phase === "retained";
  const uncertain = stage.phase === "unreachable" || stage.phase === "cleanup_failed";
  const ready = stage.phase === "ready";
  const cancelling = stage.phase === "cancelling";
  const active = !failed && !cancelled && !retained && !ready && !uncertain;
  useEffect(() => {
    if (!active) return;
    const timer = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(timer);
  }, [active]);
  useEffect(() => { setDetails(false); setNow(Date.now()); }, [stage.id]);
  const elapsed = Math.max(0, Math.floor((now - stage.startedAt) / 1000));
  const slow = active && !cancelling && elapsed >= 15;
  const headline = failed ? "worktreeSetupPage.couldNotCreateWorktree" : cancelled ? "worktreeSetupPage.creationCancelled" : retained || ready ? "worktreeSetupPage.workspaceReady" : uncertain ? "worktreeSetupPage.confirmCreationOutcome" : cancelling ? "worktreeSetupPage.cancellingCreation" : "worktreeSetupPage.creatingWorktree";
  const description = failed ? "worktreeSetupPage.theTaskHasNotBeen" : cancelled ? "worktreeSetupPage.theTaskHasNotBeen2" : retained ? "worktreeSetupPage.theWorkspaceWasAlreadyCreated" : ready ? "worktreeSetupPage.openingTheConversationAndStarting" : uncertain ? "worktreeSetupPage.theOperationOrCleanupOutcome" : cancelling ? "worktreeSetupPage.stoppingTheOperationAndCleaning" : slow ? "worktreeSetupPage.creationIsTakingLongerCompletion" : "worktreeSetupPage.yourTaskWillStartAutomatically";
  const error = stage.code ? reason(new HttpError(409, stage.detail || stage.code, { code: stage.code })) : "";
  const icon = ready || retained ? "check" : failed || uncertain ? "warning" : cancelled ? "close" : "clock";
  const phase = error || t(phases[stage.phase] || "worktreeSetupPage.waitingForConfirmation");
  return <section className="worktree-setup" data-phase={stage.phase} aria-label={t("worktreeSetupPage.preparingWorkspace")}>
    <header className="worktree-setup-header"><span className="worktree-setup-context"><StudioIcon name="folder"/><strong>{workspaceName(stage.sourceRoot)}</strong><span>{t("common.isolatedWorktree")}</span></span><h1 ref={heading} tabIndex={-1} aria-live="polite">{t(headline)}</h1><p className="worktree-setup-description">{t(description)}</p></header>
    <div className="worktree-setup-phase"><span>{active ? <span className="worktree-spinner"/> : <StudioIcon name={icon}/>}<span>{phase}</span></span>{active && <span className="worktree-setup-elapsed">{t("worktreeSetupPage.waitingS", { n: elapsed })}</span>}</div>
    {slow && <p className="worktree-setup-slow" role="status">{t("worktreeSetupPage.stillWaitingForCreationYou")}</p>}
    {stage.phase === "cleanup_failed" && <p className="worktree-setup-slow" role="note">{t("worktreeSetupPage.saveYourTaskAndAttachments")}</p>}
    <div className="worktree-setup-branches"><dl><dt>{t("common.sourceBranch")}</dt><dd title={stage.ref}><StudioIcon name="branch"/>{compactWorkspaceValue(stage.ref.replace(/^refs\/(heads|remotes)\//, ""), 48)}</dd><dt>{t("worktreeSetupPage.taskBranch")}</dt><dd title={stage.branch}><StudioIcon name="branch"/>{stage.branch ? compactWorkspaceValue(stage.branch, 48) : t("worktreeSetupPage.createAutomatically")}</dd></dl></div>
    <div className="worktree-setup-draft"><span className="worktree-setup-label">{t("worktreeSetupPage.yourTask")}</span><p className="worktree-setup-task">{task || t("worktreeSetupPage.seeTheAttachments")}</p>{files.length > 0 && <div className="worktree-setup-files"><span className="worktree-setup-label">{t("worktreeSetupPage.attachments", { n: files.length })}</span>{files.map((file, index) => <div key={`${index}:${file}`}><StudioIcon name="file"/><span>{file}</span><small>{t(ready ? "worktreeSetupPage.pendingHandoff" : failed || cancelled || retained || uncertain ? "worktreeSetupPage.retained" : "common.toSend")}</small></div>)}</div>}</div>
    <div className="worktree-setup-location"><WorkspacePath path={failed || cancelled ? stage.sourceRoot : stage.workspaceRoot || stage.sourceRoot}/></div>
    {details && <div className="worktree-setup-details"><dl><dt>{t("worktreeSetupPage.operationID")}</dt><dd>{stage.id}<CopyButton text={stage.id} iconOnly label={t("worktreeSetupPage.copyOperationID")}/></dd><dt>{t("worktreeSetupPage.currentState")}</dt><dd>{stage.phase}</dd></dl>{stage.detail && <pre>{stage.detail}</pre>}</div>}
    <footer className="worktree-setup-actions"><button className="worktree-setup-detail-button" data-action="workspace.prepare-details" onClick={() => setDetails(!details)} aria-expanded={details}><StudioIcon name="file"/>{t(details ? "worktreeSetupPage.hideDetails" : "worktreeSetupPage.viewDetails")}</button><div>{cancelled || retained || failed ? <button className="btn" data-action="workspace.prepare-edit" disabled={busy} onClick={onEdit}>{t("worktreeSetupPage.backToEditing")}</button> : null}{failed || (uncertain && stage.phase !== "cleanup_failed") ? <button className="btn" data-primary="" data-action="workspace.isolate" disabled={busy} onClick={onRetry}><StudioIcon name="refresh"/>{t(failed ? "worktreeSetupPage.retryCreation" : "worktreeSetupPage.checkAgain")}</button> : null}{ready ? <span className="worktree-setup-handoff"><StudioIcon name="check"/>{t("worktreeSetupPage.handingOverTheTaskAnd")}</span> : active || stage.phase === "unreachable" ? <button className="btn" data-action="workspace.prepare-cancel" disabled={cancelling} onClick={onCancel}>{t(cancelling ? "worktreeSetupPage.cancelling" : "worktreeSetupPage.cancelCreation")}</button> : null}</div></footer>
  </section>;
}
