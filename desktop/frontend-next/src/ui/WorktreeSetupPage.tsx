import { useEffect, useRef, useState } from "react";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import { HttpError } from "../port/port";
import type { WorktreePreparation } from "../port/workspace";
import { StudioIcon } from "./StudioIcon";
import { CopyButton } from "./CopyButton";
import { WorkspacePath, compactWorkspaceValue, workspaceName } from "./WorkspacePath";

const phases: Record<string, string> = {
  starting: "session.submitting_creation_request", validating: "session.checking_source_branch", creating: "session.creating_workspace",
  checking_out: "session.checking_out_committed_files", copying_attachments: "session.copying_task_attachments", binding: "session.binding_workspace",
  cleaning: "session.cleaning_up_incomplete_workspace", cancelling: "session.stopping_and_cleaning_up", ready: "session.workspace_bound",
  cleanup_failed: "session.cleanup_did_not_finish", failed: "session.creation_did_not_finish", cancelled: "session.creation_stopped_and_workspace_cleaned",
  retained: "session.workspace_bound", unreachable: "session.waiting_for_confirmation",
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
  const headline = failed ? "session.could_not_create_worktree" : cancelled ? "session.creation_cancelled" : retained || ready ? "session.workspace_ready" : uncertain ? "session.confirm_creation_outcome" : cancelling ? "session.cancelling_creation" : "session.creating_worktree";
  const description = failed ? "session.the_task_has_not_been_2" : cancelled ? "session.the_task_has_not_been" : retained ? "session.the_workspace_was_already_created" : ready ? "session.opening_the_conversation_and_starting" : uncertain ? "session.the_operation_or_cleanup_outcome" : cancelling ? "session.stopping_the_operation_and_cleaning" : slow ? "session.creation_is_taking_longer_completion" : "session.your_task_will_start_automatically";
  const error = stage.code ? reason(new HttpError(409, stage.detail || stage.code, { code: stage.code })) : "";
  const icon = ready || retained ? "check" : failed || uncertain ? "warning" : cancelled ? "close" : "clock";
  const phase = error || t(phases[stage.phase] || "session.waiting_for_confirmation");
  return <section className="worktree-setup" data-phase={stage.phase} aria-label={t("session.preparing_workspace")}>
    <header className="worktree-setup-header"><span className="worktree-setup-context"><StudioIcon name="folder"/><strong>{workspaceName(stage.sourceRoot)}</strong><span>{t("common.isolated_worktree")}</span></span><h1 ref={heading} tabIndex={-1} aria-live="polite">{t(headline)}</h1><p className="worktree-setup-description">{t(description)}</p></header>
    <div className="worktree-setup-phase"><span>{active ? <span className="worktree-spinner"/> : <StudioIcon name={icon}/>}<span>{phase}</span></span>{active && <span className="worktree-setup-elapsed">{t("session.waiting_n_s", { n: elapsed })}</span>}</div>
    {slow && <p className="worktree-setup-slow" role="status">{t("session.still_waiting_for_creation_you")}</p>}
    {stage.phase === "cleanup_failed" && <p className="worktree-setup-slow" role="note">{t("session.save_your_task_and_attachments")}</p>}
    <div className="worktree-setup-branches"><dl><dt>{t("common.source_branch")}</dt><dd title={stage.ref}><StudioIcon name="branch"/>{compactWorkspaceValue(stage.ref.replace(/^refs\/(heads|remotes)\//, ""), 48)}</dd><dt>{t("session.task_branch")}</dt><dd title={stage.branch}><StudioIcon name="branch"/>{stage.branch ? compactWorkspaceValue(stage.branch, 48) : t("session.create_automatically")}</dd></dl></div>
    <div className="worktree-setup-draft"><span className="worktree-setup-label">{t("session.your_task")}</span><p className="worktree-setup-task">{task || t("session.see_the_attachments")}</p>{files.length > 0 && <div className="worktree-setup-files"><span className="worktree-setup-label">{t("session.attachments_n", { n: files.length })}</span>{files.map((file, index) => <div key={`${index}:${file}`}><StudioIcon name="file"/><span>{file}</span><small>{t(ready ? "session.pending_handoff" : failed || cancelled || retained || uncertain ? "session.retained" : "common.to_send")}</small></div>)}</div>}</div>
    <div className="worktree-setup-location"><WorkspacePath path={failed || cancelled ? stage.sourceRoot : stage.workspaceRoot || stage.sourceRoot}/></div>
    {details && <div className="worktree-setup-details"><dl><dt>{t("session.operation_id")}</dt><dd>{stage.id}<CopyButton text={stage.id} iconOnly label={t("session.copy_operation_id")}/></dd><dt>{t("session.current_state")}</dt><dd>{stage.phase}</dd></dl>{stage.detail && <pre>{stage.detail}</pre>}</div>}
    <footer className="worktree-setup-actions"><button className="worktree-setup-detail-button" data-action="workspace.prepare-details" onClick={() => setDetails(!details)} aria-expanded={details}><StudioIcon name="file"/>{t(details ? "session.hide_details" : "session.view_details")}</button><div>{cancelled || retained || failed ? <button className="btn" data-action="workspace.prepare-edit" disabled={busy} onClick={onEdit}>{t("session.back_to_editing")}</button> : null}{failed || (uncertain && stage.phase !== "cleanup_failed") ? <button className="btn" data-primary="" data-action="workspace.isolate" disabled={busy} onClick={onRetry}><StudioIcon name="refresh"/>{t(failed ? "session.retry_creation" : "session.check_again")}</button> : null}{ready ? <span className="worktree-setup-handoff"><StudioIcon name="check"/>{t("session.handing_over_the_task_and")}</span> : active || stage.phase === "unreachable" ? <button className="btn" data-action="workspace.prepare-cancel" disabled={cancelling} onClick={onCancel}>{t(cancelling ? "session.cancelling" : "session.cancel_creation")}</button> : null}</div></footer>
  </section>;
}
