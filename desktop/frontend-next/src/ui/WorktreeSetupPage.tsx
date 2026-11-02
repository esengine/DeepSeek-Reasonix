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
  starting: "正在提交创建请求", validating: "正在检查起始分支", creating: "正在创建工作目录",
  checking_out: "正在检出已提交文件", copying_attachments: "正在复制任务附件", binding: "正在绑定工作目录",
  cleaning: "正在清理未完成目录", cancelling: "正在中止并清理", ready: "工作目录已绑定",
  cleanup_failed: "清理未完成", failed: "创建未完成", cancelled: "创建已中止，目录已清理",
  retained: "工作目录已绑定", unreachable: "正在等待状态确认",
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
  const headline = failed ? "worktree 创建失败" : cancelled ? "已取消创建" : retained || ready ? "工作目录已就绪" : uncertain ? "需要确认创建结果" : cancelling ? "正在取消创建…" : "正在创建 worktree…";
  const description = failed ? "任务尚未发送，输入和附件已为你保留。" : cancelled ? "任务尚未发送，可以继续编辑。" : retained ? "目录已经创建成功，取消未能撤回目录切换。任务尚未发送。" : ready ? "即将进入对话，自动开始任务。" : uncertain ? "暂不能确认操作或清理结果，请先查看详情并核对。" : cancelling ? "正在中止操作并清理未完成的目录。" : slow ? "创建耗时较长，尚未收到完成确认。" : "准备完成后将自动开始任务。";
  const error = stage.code ? reason(new HttpError(409, stage.detail || stage.code, { code: stage.code })) : "";
  const icon = ready || retained ? "check" : failed || uncertain ? "warning" : cancelled ? "close" : "clock";
  const phase = error || t(phases[stage.phase] || "正在等待状态确认");
  return <section className="worktree-setup" data-phase={stage.phase} aria-label={t("工作目录准备")}>
    <header className="worktree-setup-header"><span className="worktree-setup-context"><StudioIcon name="folder"/><strong>{workspaceName(stage.sourceRoot)}</strong><span>{t("独立工作树")}</span></span><h1 ref={heading} tabIndex={-1} aria-live="polite">{t(headline)}</h1><p className="worktree-setup-description">{t(description)}</p></header>
    <div className="worktree-setup-phase"><span>{active ? <span className="worktree-spinner"/> : <StudioIcon name={icon}/>}<span>{phase}</span></span>{active && <span className="worktree-setup-elapsed">{t("已等待 {n} 秒", { n: elapsed })}</span>}</div>
    {slow && <p className="worktree-setup-slow" role="status">{t("仍在等待创建完成。你可以继续等待、查看详情，或取消创建。")}</p>}
    {stage.phase === "cleanup_failed" && <p className="worktree-setup-slow" role="note">{t("请先保存任务与附件，在终端核对并处理保留的目录和任务分支。确认处理完成后，在会话菜单中关闭此会话，再从源目录新建会话重试。")}</p>}
    <div className="worktree-setup-branches"><dl><dt>{t("起始分支")}</dt><dd title={stage.ref}><StudioIcon name="branch"/>{compactWorkspaceValue(stage.ref.replace(/^refs\/(heads|remotes)\//, ""), 48)}</dd><dt>{t("任务分支")}</dt><dd title={stage.branch}><StudioIcon name="branch"/>{stage.branch ? compactWorkspaceValue(stage.branch, 48) : t("自动创建")}</dd></dl></div>
    <div className="worktree-setup-draft"><span className="worktree-setup-label">{t("用户任务")}</span><p className="worktree-setup-task">{task || t("请查看附件")}</p>{files.length > 0 && <div className="worktree-setup-files"><span className="worktree-setup-label">{t("附件（{n}）", { n: files.length })}</span>{files.map((file, index) => <div key={`${index}:${file}`}><StudioIcon name="file"/><span>{file}</span><small>{t(ready ? "待交接" : failed || cancelled || retained || uncertain ? "已保留" : "待发送")}</small></div>)}</div>}</div>
    <div className="worktree-setup-location"><WorkspacePath path={failed || cancelled ? stage.sourceRoot : stage.workspaceRoot || stage.sourceRoot}/></div>
    {details && <div className="worktree-setup-details"><dl><dt>{t("操作编号")}</dt><dd>{stage.id}<CopyButton text={stage.id} iconOnly label={t("复制操作编号")}/></dd><dt>{t("当前状态")}</dt><dd>{stage.phase}</dd></dl>{stage.detail && <pre>{stage.detail}</pre>}</div>}
    <footer className="worktree-setup-actions"><button className="worktree-setup-detail-button" data-action="workspace.prepare-details" onClick={() => setDetails(!details)} aria-expanded={details}><StudioIcon name="file"/>{t(details ? "收起详情" : "查看详情")}</button><div>{cancelled || retained || failed ? <button className="btn" data-action="workspace.prepare-edit" disabled={busy} onClick={onEdit}>{t("返回编辑")}</button> : null}{failed || (uncertain && stage.phase !== "cleanup_failed") ? <button className="btn" data-primary="" data-action="workspace.isolate" disabled={busy} onClick={onRetry}><StudioIcon name="refresh"/>{t(failed ? "重试创建" : "再次核对")}</button> : null}{ready ? <span className="worktree-setup-handoff"><StudioIcon name="check"/>{t("任务和附件正在交接…")}</span> : active || stage.phase === "unreachable" ? <button className="btn" data-action="workspace.prepare-cancel" disabled={cancelling} onClick={onCancel}>{t(cancelling ? "正在取消…" : "取消创建")}</button> : null}</div></footer>
  </section>;
}
