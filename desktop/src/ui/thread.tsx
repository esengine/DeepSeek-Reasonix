import type { ReactNode } from "react";
import { I } from "../icons";
import type { AssistantSegment, ActivePlan, PendingPlan, PendingCheckpoint, PendingRevision, PendingConfirm, PendingChoice } from "../App";
import { AssistantText, PlanCardView, ReasoningCard, ShellCard, ToolCard, type PlanItem } from "./cards";
import { ApprovalCard, TaskCard, type TaskStepView } from "./extra-cards";

export function TurnDivider({ label }: { label: string }) {
  return (
    <div className="turn-divider">
      <span>{label}</span>
      <span className="line" />
    </div>
  );
}

export function UserMsg({ text, time }: { text: string; time?: string }) {
  return (
    <div className="msg user">
      <div className="avatar">YOU</div>
      <div className="body">
        <div className="who">
          <span className="name">你</span>
          {time ? <span className="time">{time}</span> : null}
        </div>
        <div className="msg-text">{text}</div>
      </div>
    </div>
  );
}

export function AssistantMsg({
  segments,
  pending,
  model,
  time,
  onApproveConfirm,
  onRejectConfirm,
  onAlwaysAllowConfirm,
  pendingConfirms,
}: {
  segments: AssistantSegment[];
  pending: boolean;
  model?: string;
  time?: string;
  onApproveConfirm: (id: number) => void;
  onRejectConfirm: (id: number) => void;
  onAlwaysAllowConfirm: (id: number, prefix: string) => void;
  pendingConfirms: PendingConfirm[];
}) {
  return (
    <div className="msg assistant">
      <div className="avatar">DS</div>
      <div className="body">
        <div className="who">
          <span className="name">Reasonix</span>
          {model ? <span className="model">{model}</span> : null}
          {time ? <span className="time">{time}</span> : null}
        </div>
        {segments.map((s, i) => {
          if (s.kind === "text") {
            if (!s.text.trim()) return null;
            return <AssistantText key={i} text={s.text} />;
          }
          if (s.kind === "reasoning") {
            return (
              <ReasoningCard
                key={i}
                text={s.text}
                streaming={pending && i === segments.length - 1}
              />
            );
          }
          // tool segment
          const pendingConfirm =
            (s.name === "run_command" || s.name === "run_background") && s.result === undefined
              ? pendingConfirms.find((c) => c.command === extractCommand(s.args))
              : undefined;
          if (s.name === "run_command" || s.name === "run_background") {
            const cmd = extractCommand(s.args) ?? s.args;
            const state: "await" | "running" | "done" | "failed" =
              s.result === undefined
                ? pendingConfirm
                  ? "await"
                  : "running"
                : s.ok === false
                  ? "failed"
                  : "done";
            return (
              <ShellCard
                key={i}
                command={cmd}
                output={s.result}
                state={state}
                durationMs={s.durationMs}
                onApprove={pendingConfirm ? () => onApproveConfirm(pendingConfirm.id) : undefined}
                onReject={pendingConfirm ? () => onRejectConfirm(pendingConfirm.id) : undefined}
                onAlwaysAllow={
                  pendingConfirm
                    ? () => {
                        const prefix = cmd.split(/\s+/)[0] ?? cmd;
                        onAlwaysAllowConfirm(pendingConfirm.id, `${prefix} *`);
                      }
                    : undefined
                }
              />
            );
          }
          return (
            <ToolCard
              key={i}
              name={s.name}
              args={s.args}
              result={s.result}
              ok={s.ok}
              durationMs={s.durationMs}
            />
          );
        })}
      </div>
    </div>
  );
}

function extractCommand(args: string): string | undefined {
  if (!args) return undefined;
  try {
    const v = JSON.parse(args);
    if (v && typeof v === "object" && typeof v.command === "string") return v.command;
  } catch {
    // ignore
  }
  return undefined;
}

export function PlanBanner({
  plan,
  onDismiss,
}: {
  plan: ActivePlan;
  onDismiss?: () => void;
}) {
  const total = plan.steps.length || 1;
  const done = plan.completedStepIds.length;
  const pct = (done / total) * 100;
  const current = plan.steps.find((s) => !plan.completedStepIds.includes(s.id));
  return (
    <div className="plan-banner">
      <span className="ico">
        <I.list size={14} />
      </span>
      <div className="body">
        <div className="t">
          计划执行中 · step {Math.min(done + 1, total)} / {total}
          {current ? ` — ${current.title}` : ""}
        </div>
        <div className="s">{plan.plan}</div>
      </div>
      <div className="prog">
        <div className="meter-mini">
          <span style={{ width: `${pct}%` }} />
        </div>
        {onDismiss ? (
          <button type="button" onClick={onDismiss}>
            收起
          </button>
        ) : null}
      </div>
    </div>
  );
}

export function ActivePlanCard({ plan }: { plan: ActivePlan }) {
  const done = new Set(plan.completedStepIds);
  const items: PlanItem[] = plan.steps.map((s) => {
    let status: PlanItem["status"];
    if (done.has(s.id)) status = "done";
    else if (s === plan.steps.find((x) => !done.has(x.id))) status = "active";
    else status = "todo";
    return {
      id: s.id,
      status,
      text: s.title,
      tool: s.action,
      note: s.risk ? `risk: ${s.risk}` : undefined,
    };
  });
  return <PlanCardView items={items} title="活动计划" />;
}

// ---- Approval bindings ----

export function PlanApprovalCard({
  p,
  onApprove,
  onRefine,
  onCancel,
}: {
  p: PendingPlan;
  onApprove: () => void;
  onRefine: () => void;
  onCancel: () => void;
}) {
  const stepCount = p.steps?.length ?? 0;
  const sub = stepCount > 0 ? `${stepCount} step` : undefined;
  return (
    <ApprovalCard
      kind="plan confirmation"
      tone="info"
      title="开始执行计划"
      sub={sub}
      body={
        <>
          {p.summary ? <div style={{ marginBottom: 6 }}>{p.summary}</div> : null}
          <div style={{ whiteSpace: "pre-wrap" }}>{p.plan}</div>
        </>
      }
      meta={`plan/#${p.id}`}
      primaryLabel="批准"
      secondaryLabel="取消"
      tertiaryLabel="细化"
      onPrimary={onApprove}
      onSecondary={onCancel}
      onTertiary={onRefine}
    />
  );
}

export function CheckpointApprovalCard({
  c,
  onContinue,
  onRevise,
  onStop,
}: {
  c: PendingCheckpoint;
  onContinue: () => void;
  onRevise: () => void;
  onStop: () => void;
}) {
  return (
    <ApprovalCard
      kind="plan checkpoint"
      tone="brand"
      title={c.title ?? `step ${c.completed} / ${c.total} 完成`}
      sub={`检查点 · ${c.completed} / ${c.total}`}
      body={
        <>
          <div style={{ whiteSpace: "pre-wrap" }}>{c.result}</div>
          {c.notes ? (
            <div style={{ marginTop: 8, fontSize: 11.5, color: "var(--muted)" }}>{c.notes}</div>
          ) : null}
        </>
      }
      meta={`checkpoint · ${c.stepId}`}
      primaryLabel="继续"
      secondaryLabel="停止"
      tertiaryLabel="修订"
      onPrimary={onContinue}
      onSecondary={onStop}
      onTertiary={onRevise}
    />
  );
}

export function RevisionApprovalCard({
  r,
  onAccept,
  onReject,
}: {
  r: PendingRevision;
  onAccept: () => void;
  onReject: () => void;
}) {
  return (
    <ApprovalCard
      kind="plan revision"
      tone="warn"
      title="计划重写"
      sub={`保留已完成步骤 · 新增 ${r.remainingSteps.length} step`}
      body={
        <>
          <div style={{ marginBottom: 8 }}>{r.reason}</div>
          {r.summary ? (
            <div style={{ fontSize: 11.5, color: "var(--muted)", marginBottom: 8 }}>{r.summary}</div>
          ) : null}
          <ul style={{ margin: 0, paddingLeft: 18 }}>
            {r.remainingSteps.map((s) => (
              <li key={s.id} style={{ fontSize: 12, marginBottom: 2 }}>
                {s.title}
                {s.risk ? (
                  <span
                    style={{
                      marginLeft: 6,
                      fontSize: 10,
                      color:
                        s.risk === "high"
                          ? "var(--tone-err)"
                          : s.risk === "med"
                            ? "var(--tone-warn)"
                            : "var(--muted)",
                    }}
                  >
                    [{s.risk}]
                  </span>
                ) : null}
              </li>
            ))}
          </ul>
        </>
      }
      meta="reason · runtime constraint"
      primaryLabel="批准重写"
      secondaryLabel="保留原方案"
      onPrimary={onAccept}
      onSecondary={onReject}
    />
  );
}

export function ConfirmApprovalCard({
  c,
  onAllow,
  onAlwaysAllow,
  onDeny,
}: {
  c: PendingConfirm;
  onAllow: () => void;
  onAlwaysAllow: (prefix: string) => void;
  onDeny: () => void;
}) {
  const isBackground = c.kind === "run_background";
  const firstWord = c.command.split(/\s+/)[0] ?? c.command;
  return (
    <ApprovalCard
      kind="shell confirmation"
      tone="warn"
      title={isBackground ? "后台运行命令" : "运行命令"}
      sub={c.command.length > 80 ? `${c.command.slice(0, 80)}…` : c.command}
      preview={
        <>
          <span style={{ color: "var(--accent)" }}>$</span> {c.command}
        </>
      }
      meta={`risk · 中 · ${c.kind}`}
      primaryLabel="执行"
      secondaryLabel="拒绝"
      tertiaryLabel={`始终允许 "${firstWord} *"`}
      onPrimary={onAllow}
      onSecondary={onDeny}
      onTertiary={() => onAlwaysAllow(`${firstWord} *`)}
    />
  );
}

export function PathAccessApprovalCard({
  p,
  onAllow,
  onAlwaysAllow,
  onDeny,
}: {
  p: {
    id: number;
    path: string;
    intent: "read" | "write";
    toolName: string;
    sandboxRoot: string;
    allowPrefix: string;
  };
  onAllow: () => void;
  onAlwaysAllow: (prefix: string) => void;
  onDeny: () => void;
}) {
  const intentText = p.intent === "write" ? "写入" : "读取";
  return (
    <ApprovalCard
      kind="path access"
      tone="warn"
      title={`${intentText}沙盒外的路径`}
      sub={p.path}
      preview={
        <>
          <div>{p.toolName} → {p.path}</div>
          <div style={{ color: "var(--muted)", marginTop: 4 }}>
            workspace: {p.sandboxRoot}
          </div>
        </>
      }
      meta={`risk · 中 · ${p.intent}`}
      primaryLabel={intentText === "写入" ? "允许写入" : "允许读取"}
      secondaryLabel="拒绝"
      tertiaryLabel={`始终允许 ${p.allowPrefix}`}
      onPrimary={onAllow}
      onSecondary={onDeny}
      onTertiary={() => onAlwaysAllow(p.allowPrefix)}
    />
  );
}

export function ChoiceApprovalCard({
  c,
  onPick,
  onCancel,
}: {
  c: PendingChoice;
  onPick: (optionId: string) => void;
  onCancel: () => void;
}) {
  return (
    <ApprovalCard
      kind="user choice"
      tone="info"
      title={c.question}
      sub={`${c.options.length} 个选项`}
      body={
        <div style={{ display: "flex", flexDirection: "column", gap: 6 }}>
          {c.options.map((o) => (
            <button
              key={o.id}
              type="button"
              className="btn"
              style={{ justifyContent: "flex-start", textAlign: "left" }}
              onClick={() => onPick(o.id)}
            >
              <div>
                <div style={{ fontWeight: 600 }}>{o.title}</div>
                {o.summary ? (
                  <div style={{ fontSize: 11, color: "var(--muted)", marginTop: 2 }}>
                    {o.summary}
                  </div>
                ) : null}
              </div>
            </button>
          ))}
        </div>
      }
      primaryLabel="取消"
      onPrimary={onCancel}
    />
  );
}

export function activePlanToTaskSteps(plan: ActivePlan): TaskStepView[] {
  const done = new Set(plan.completedStepIds);
  return plan.steps.map((s, i) => ({
    n: String(i + 1),
    state: done.has(s.id) ? "done" : i === plan.completedStepIds.length ? "running" : "queued",
    label: s.title,
    hint: s.action,
    durationLabel: undefined,
  }));
}

export function ActivePlanTaskCard({ plan }: { plan: ActivePlan }) {
  return (
    <TaskCard
      title="活动计划"
      subtitle={plan.summary}
      steps={activePlanToTaskSteps(plan)}
    />
  );
}

export function HeaderHint({ children }: { children: ReactNode }) {
  return <div className="msg-text">{children}</div>;
}
