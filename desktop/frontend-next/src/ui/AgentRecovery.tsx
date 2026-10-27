import { useEffect, useRef, useState } from "react";
import type { AgentPort } from "../port/port";
import type { FleetRecoveryPlan } from "../port/subagent";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";

export function AgentRecovery({ port, runRef }: { port: AgentPort; runRef: string }) {
  const [plans, setPlans] = useState<FleetRecoveryPlan[] | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [queued, setQueued] = useState("");
  const lifetime = useRef(0);
  const pending = useRef(false);
  useEffect(() => {
    lifetime.current++; pending.current = false;
    setPlans(null); setError(""); setQueued(""); setBusy(false);
    return () => { lifetime.current++; };
  }, [port, runRef]);
  const act = async (plan?: FleetRecoveryPlan) => {
    if (pending.current) return;
    const generation = lifetime.current;
    pending.current = true; setBusy(true); setError("");
    try {
      if (plan) {
        await port.requestFleetRecovery(runRef, plan.id, plan.snapshot);
        if (lifetime.current === generation) setQueued(plan.id);
      } else {
        const rows = await port.fleetRecoveryPlans(runRef);
        if (lifetime.current === generation) { setPlans(rows); setQueued(""); }
      }
    } catch (cause) {
      if (lifetime.current === generation) setError(reason(cause));
    } finally {
      if (lifetime.current === generation) { pending.current = false; setBusy(false); }
    }
  };
  if (!port.fleetRecoveryPlans) return null;
  return <section className="agentrun-guidance" aria-label={t("任务图恢复")}>
    <div className="agentrun-guidance-actions">
      <span>{t("任务图恢复")}</span>
      <button className="btn btn--small" data-action="agent.recovery.inspect" data-target={runRef} disabled={busy} onClick={() => void act()}>{t("查看恢复计划")}</button>
    </div>
    {plans?.length === 0 && <p>{t("没有可恢复的保存计划。旧任务需要重新选择任务与已有结果。")}</p>}
    {plans?.map(plan => <div key={plan.id}>
      {plan.legacy && <p>{t("旧计划重建：按当前权限和新预算重新确认，不推测缺失的历史参数。")}</p>}
      <ol>{plan.tasks.map(task => <li key={task.id}>
        <span>{task.description || task.id}</span>{" · "}<span>{t(task.state === "completed" ? "复用已完成结果" : "重新执行未完成任务")}</span>
        {task.prompt && <details><summary>{t("任务内容")}</summary><p className="agentrun-recovery-prompt">{task.prompt}</p></details>}
      </li>)}</ol>
      {plan.reason && <p role="alert">{plan.reason}</p>}
      <p>{t("未完成任务可能已有部分写入。恢复会检查当前权限，不会自动重复已完成节点。")}</p>
      <button className="btn btn--small" data-action="agent.recovery.resume" data-target={plan.id} disabled={busy || !plan.ready || queued === plan.id} onClick={() => void act(plan)}>{t("请求恢复执行")}</button>
    </div>)}
    {queued && <p role="status">{t("恢复请求已加入当前会话，实际执行进度将在会话中显示。")}</p>}
    {error && <p role="alert">{error}</p>}
  </section>;
}
