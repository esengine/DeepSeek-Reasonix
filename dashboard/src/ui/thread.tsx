import { derivePrefix } from "@reasonix/core-utils/derive-prefix";
import { memo, useState, type ReactNode } from "react";
import { Copy } from "lucide-react";
import { I } from "../icons";
import { t, useLang } from "../i18n";
import type { AssistantSegment, ActivePlan, PendingPlan, PendingCheckpoint, PendingRevision, PendingConfirm, PendingChoice, SkillOrigin } from "../App";
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

export const UserMsg = memo(function UserMsg({
  text,
  time,
  skill,
}: {
  text: string;
  time?: string;
  skill?: SkillOrigin;
}) {
  useLang();
  const [copied, setCopied] = useState(false);
  const onCopy = async () => {
    try {
      await navigator.clipboard.writeText(text);
      setCopied(true);
      setTimeout(() => setCopied(false), 1200);
    } catch {
      /* ignore */
    }
  };
  return (
    <div className="msg user">
      <div className="avatar">YOU</div>
      <div className="body">
        <div className="who">
          <span className="name">{t("thread.you")}</span>
          {skill ? (
            <span className="skill-chip" title={`skill · ${skill.runAs}`}>
              <I.zap size={10} /> /{skill.name}
              {skill.runAs === "subagent" ? <span className="sub">{t("thread.subagent")}</span> : null}
            </span>
          ) : null}
          {time ? <span className="time">{time}</span> : null}
        </div>
        <div className="msg-text">{text}</div>
        <div className="msg-actions">
          <button
            type="button"
            className={`copy-btn ${copied ? "done" : ""}`}
            onClick={onCopy}
            title={t("thread.copyMessage")}
          >
            <Copy size={11} />
            {copied ? t("markdown.copied") : null}
          </button>
        </div>
      </div>
    </div>
  );
});

export const AssistantMsg = memo(function AssistantMsg({
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
  const [copied, setCopied] = useState(false);
  const content = segments
    .filter((s): s is AssistantSegment & { kind: "text" } => s.kind === "text")
    .map((s) => s.text)
    .join("\n\n");
  const onCopy = async () => {
    try {
      await navigator.clipboard.writeText(content);
      setCopied(true);
      setTimeout(() => setCopied(false), 1200);
    } catch {
      /* ignore */
    }
  };
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
                        onAlwaysAllowConfirm(pendingConfirm.id, derivePrefix(cmd));
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
        {content ? (
          <div className="msg-actions">
            <button
              type="button"
              className={`copy-btn ${copied ? "done" : ""}`}
              onClick={onCopy}
              title={t("thread.copyResponse")}
            >
              <Copy size={11} />
              {copied ? t("markdown.copied") : null}
            </button>
          </div>
        ) : null}
      </div>
    </div>
  );
});

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
  useLang();
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
          {t("thread.planRunning", { step: Math.min(done + 1, total), total })}
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
            {t("thread.collapse")}
          </button>
        ) : null}
      </div>
    </div>
  );
}

export function ActivePlanCard({ plan }: { plan: ActivePlan }) {
  useLang();
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
      note: s.risk ? `${t("thread.risk")}: ${s.risk}` : undefined,
    };
  });
  return <PlanCardView items={items} title={t("thread.activePlan")} />;
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
  useLang();
  const stepCount = p.steps?.length ?? 0;
  const sub = stepCount > 0 ? t("thread.planStepCount", { count: stepCount }) : undefined;
  return (
    <ApprovalCard
      kind={t("thread.planConfirmationKind")}
      tone="info"
      title={t("thread.startPlan")}
      sub={sub}
      body={
        <>
          {p.summary ? <div style={{ marginBottom: 6 }}>{p.summary}</div> : null}
          <div style={{ whiteSpace: "pre-wrap" }}>{p.plan}</div>
        </>
      }
      meta={`plan/#${p.id}`}
      primaryLabel={t("thread.approve")}
      secondaryLabel={t("thread.cancel")}
      tertiaryLabel={t("thread.refine")}
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
  useLang();
  return (
    <ApprovalCard
      kind={t("thread.checkpointKind")}
      tone="brand"
      title={c.title ?? t("thread.checkpointTitle", { completed: c.completed, total: c.total })}
      sub={t("thread.checkpointSub", { completed: c.completed, total: c.total })}
      body={
        <>
          <div style={{ whiteSpace: "pre-wrap" }}>{c.result}</div>
          {c.notes ? (
            <div style={{ marginTop: 8, fontSize: 11.5, color: "var(--muted)" }}>{c.notes}</div>
          ) : null}
        </>
      }
      meta={`checkpoint · ${c.stepId}`}
      primaryLabel={t("thread.continue")}
      secondaryLabel={t("thread.stop")}
      tertiaryLabel={t("thread.revise")}
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
  useLang();
  return (
    <ApprovalCard
      kind={t("thread.planRevisionKind")}
      tone="warn"
      title={t("thread.rewritePlan")}
      sub={t("thread.keepSteps", { n: r.remainingSteps.length })}
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
      meta={t("thread.revisionMeta")}
      primaryLabel={t("thread.approveRewrite")}
      secondaryLabel={t("thread.keepOriginal")}
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
  useLang();
  const isBackground = c.kind === "run_background";
  const prefix = derivePrefix(c.command);
  return (
    <ApprovalCard
      kind={t("thread.shellConfirmationKind")}
      tone="warn"
      title={isBackground ? t("thread.runBackgroundCommand") : t("thread.runCommand")}
      sub={c.command.length > 80 ? `${c.command.slice(0, 80)}…` : c.command}
      preview={
        <>
          <span style={{ color: "var(--accent)" }}>$</span> {c.command}
        </>
      }
      meta={t("thread.riskMedium", { kind: c.kind })}
      primaryLabel={t("thread.execute")}
      secondaryLabel={t("thread.reject")}
      tertiaryLabel={t("thread.alwaysAllow", { prefix })}
      onPrimary={onAllow}
      onSecondary={onDeny}
      onTertiary={() => onAlwaysAllow(prefix)}
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
  useLang();
  const isWrite = p.intent === "write";
  return (
    <ApprovalCard
      kind={t("thread.pathAccessKind")}
      tone="warn"
      title={isWrite ? t("thread.writePathOutsideSandbox") : t("thread.readPathOutsideSandbox")}
      sub={p.path}
      preview={
        <>
          <div>{p.toolName} → {p.path}</div>
          <div style={{ color: "var(--muted)", marginTop: 4 }}>
            workspace: {p.sandboxRoot}
          </div>
        </>
      }
      meta={t("thread.riskMedium", { kind: p.intent })}
      primaryLabel={isWrite ? t("thread.allowWrite") : t("thread.allowRead")}
      secondaryLabel={t("thread.reject")}
      tertiaryLabel={t("thread.alwaysAllowPrefix", { prefix: p.allowPrefix })}
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
  useLang();
  return (
    <ApprovalCard
      kind={t("thread.userChoiceKind")}
      tone="info"
      title={c.question}
      sub={t("thread.optionCount", { count: c.options.length })}
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
      primaryLabel={t("thread.cancel")}
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
  useLang();
  return (
    <TaskCard
      title={t("thread.activePlan")}
      subtitle={plan.summary}
      steps={activePlanToTaskSteps(plan)}
    />
  );
}

export function HeaderHint({ children }: { children: ReactNode }) {
  return <div className="msg-text">{children}</div>;
}
