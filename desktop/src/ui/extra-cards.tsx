import type { ReactNode } from "react";
import { I } from "../icons";

export type ApprovalTone = "ok" | "warn" | "danger" | "info" | "brand" | "ghost";

export function ApprovalCard({
  kind,
  tone = "info",
  title,
  sub,
  body,
  preview,
  meta,
  primaryLabel = "批准",
  secondaryLabel = "拒绝",
  tertiaryLabel,
  onPrimary,
  onSecondary,
  onTertiary,
}: {
  kind: string;
  tone?: ApprovalTone;
  title: string;
  sub?: string;
  body?: ReactNode;
  preview?: ReactNode;
  meta?: ReactNode;
  primaryLabel?: string;
  secondaryLabel?: string;
  tertiaryLabel?: string;
  onPrimary?: () => void;
  onSecondary?: () => void;
  onTertiary?: () => void;
}) {
  return (
    <div className="approval" data-tone={tone}>
      <div className="ap-head">
        <span className="ap-ico">
          <I.shield size={13} />
        </span>
        <div>
          <div className="ap-kind">{kind}</div>
          <div className="ap-title">{title}</div>
          {sub ? <div className="ap-sub">{sub}</div> : null}
        </div>
      </div>
      {body ? <div className="ap-body">{body}</div> : null}
      {preview ? <div className="ap-preview">{preview}</div> : null}
      <div className="ap-foot">
        {onPrimary ? (
          <button type="button" className="btn primary" onClick={onPrimary}>
            {primaryLabel}
          </button>
        ) : null}
        {onSecondary ? (
          <button type="button" className="btn ghost" onClick={onSecondary}>
            {secondaryLabel}
          </button>
        ) : null}
        {onTertiary && tertiaryLabel ? (
          <button type="button" className="btn ghost" onClick={onTertiary}>
            {tertiaryLabel}
          </button>
        ) : null}
        <span className="grow" />
        {meta ? <span className="meta">{meta}</span> : null}
      </div>
    </div>
  );
}

// ---- Task Card (multi-step execution from active plan) ----

export type TaskStepView = {
  n: string;
  state: "queued" | "running" | "done" | "failed" | "blocked" | "skipped";
  label: string;
  hint?: string;
  durationLabel?: string;
};

export function TaskCard({
  title,
  subtitle,
  steps,
}: {
  title: string;
  subtitle?: string;
  steps: TaskStepView[];
}) {
  const done = steps.filter((x) => x.state === "done").length;
  const pct = steps.length ? (done / steps.length) * 100 : 0;
  return (
    <div className="task-card">
      <div className="th">
        <span className="ico">
          <I.list size={13} />
        </span>
        <div>
          <div className="tt">{title}</div>
          {subtitle ? <div className="ss">{subtitle}</div> : null}
        </div>
        <span className="grow" />
        <span className="ss">
          {done}/{steps.length}
        </span>
        <div className="meter">
          <span style={{ width: `${pct}%` }} />
        </div>
      </div>
      <div className="tb">
        {steps.map((st) => (
          <div className="task-step" key={st.n} data-state={st.state}>
            <span className="nx">step.{st.n}</span>
            <span className="st" />
            <div className="l">
              {st.label}
              {st.hint ? <div className="h">{st.hint}</div> : null}
            </div>
            <span className="t">{st.durationLabel ?? "—"}</span>
          </div>
        ))}
      </div>
    </div>
  );
}

// ---- Warn / Tip / Doctor ----

export function WarnCard({ title, body }: { title: string; body: ReactNode }) {
  return (
    <div className="warn-card">
      <span className="ico">
        <I.warn size={16} />
      </span>
      <div>
        <div className="tt">{title}</div>
        <div className="ds">{body}</div>
      </div>
    </div>
  );
}

export type TipSection = { title: string; rows: ReactNode[] };

export function TipCard({
  topic,
  command,
  sections,
  footer,
}: {
  topic: string;
  command?: string;
  sections: TipSection[];
  footer?: ReactNode;
}) {
  return (
    <div className="tip-card">
      <div className="head">
        <span className="ico">
          <I.help size={12} />
        </span>
        <span className="topic">{topic}</span>
        <span className="grow" />
        {command ? <span className="pill">{command}</span> : null}
      </div>
      {sections.map((sec, i) => (
        <div className="sec" key={i}>
          <div className="stt">{sec.title}</div>
          {sec.rows.map((r, j) => (
            <div className="row" key={j}>
              {r}
            </div>
          ))}
        </div>
      ))}
      {footer ? <div className="foot">{footer}</div> : null}
    </div>
  );
}

export type DoctorRow = { s: "ok" | "warn" | "fail"; nm: string; sub: string; v: string };

export function DoctorCard({ rows, headerSubtitle }: { rows: DoctorRow[]; headerSubtitle?: string }) {
  const c = { ok: 0, warn: 0, fail: 0 };
  for (const r of rows) c[r.s]++;
  return (
    <div className="doctor-card">
      <div className="dh">
        <span className="ico">
          <I.shield size={13} />
        </span>
        <div>
          <div className="tt">/doctor · 系统体检</div>
          {headerSubtitle ? (
            <div
              className="ss"
              style={{
                fontFamily: "IBM Plex Mono, monospace",
                fontSize: 10.5,
                color: "var(--muted)",
              }}
            >
              {headerSubtitle}
            </div>
          ) : null}
        </div>
        <span className="grow" />
        <div className="summary">
          <span>
            <span className="b ok">{c.ok}</span> ok
          </span>
          <span>
            <span className="b warn">{c.warn}</span> warn
          </span>
          <span>
            <span className="b err">{c.fail}</span> fail
          </span>
        </div>
      </div>
      {rows.map((r, i) => (
        <div className="doctor-row" key={i} data-s={r.s}>
          <span className="ic" data-mark={r.s === "ok" ? "✓" : r.s === "warn" ? "!" : "✕"} />
          <div className="body">
            <div className="nm">{r.nm}</div>
            <div className="sub">{r.sub}</div>
          </div>
          <span className="v">{r.v}</span>
        </div>
      ))}
    </div>
  );
}

// ---- Usage (full) ----

export function UsageFull({
  promptTokens,
  reasoningTokens,
  outputTokens,
  cacheHitTokens,
  costLabel,
  balanceLabel,
  range,
}: {
  promptTokens: number;
  reasoningTokens: number;
  outputTokens: number;
  cacheHitTokens: number;
  costLabel: string;
  balanceLabel?: string;
  range?: string;
}) {
  const fmt = (n: number) => (n >= 1000 ? `${(n / 1000).toFixed(1)}k` : `${n}`);
  const total = promptTokens + reasoningTokens + outputTokens + cacheHitTokens || 1;
  const pct = (n: number) => Math.round((n / total) * 100);
  return (
    <div className="usage-full">
      <div className="uh">
        <div>
          <div className="tt">本会话用量</div>
          {range ? <div className="ss">{range}</div> : null}
        </div>
        <span className="grow" />
      </div>
      <div className="ub">
        <div className="ucol">
          <div className="l">prompt</div>
          <div className="v acc">{fmt(promptTokens)}</div>
          <div className="pct">{pct(promptTokens)}%</div>
        </div>
        <div className="ucol">
          <div className="l">reasoning</div>
          <div className="v vio">{fmt(reasoningTokens)}</div>
          <div className="pct">{pct(reasoningTokens)}%</div>
        </div>
        <div className="ucol">
          <div className="l">output</div>
          <div className="v ok">{fmt(outputTokens)}</div>
          <div className="pct">{pct(outputTokens)}%</div>
        </div>
        <div className="ucol">
          <div className="l">cache hit</div>
          <div className="v">{fmt(cacheHitTokens)}</div>
          <div className="pct">{pct(cacheHitTokens)}%</div>
        </div>
      </div>
      <div className="stack">
        <span className="s1" style={{ width: `${pct(promptTokens)}%` }} />
        <span className="s2" style={{ width: `${pct(reasoningTokens)}%` }} />
        <span className="s3" style={{ width: `${pct(outputTokens)}%` }} />
        <span className="s4" style={{ width: `${pct(cacheHitTokens)}%` }} />
      </div>
      <div className="uf">
        <span className="x">
          <span className="sw" style={{ background: "var(--accent)" }} />
          prompt
        </span>
        <span className="x">
          <span className="sw" style={{ background: "var(--violet)" }} />
          reasoning
        </span>
        <span className="x">
          <span className="sw" style={{ background: "var(--tone-ok)" }} />
          output
        </span>
        <span className="x">
          <span className="sw" style={{ background: "var(--border-strong)" }} />
          cache
        </span>
        <span style={{ marginLeft: "auto" }}>
          本会话花费 {costLabel}
          {balanceLabel ? ` · 余额 ${balanceLabel}` : ""}
        </span>
      </div>
    </div>
  );
}

// ---- Context window breakdown ----

export type CtxPart = { k: "system" | "tools" | "log" | "input"; label: string; value: string; widthPct: number };
export type CtxTopRow = { name: string; widthPct: number; value: string };

export function CtxCard({
  totalLabel,
  parts,
  topTools,
}: {
  totalLabel: string;
  parts: CtxPart[];
  topTools: CtxTopRow[];
}) {
  return (
    <div className="ctx-card">
      <div className="h">
        <span className="tt">上下文窗口</span>
        <span className="grow" />
        <span className="v">{totalLabel}</span>
      </div>
      <div className="bar">
        {parts.map((p) => (
          <span key={p.k} className={p.k} style={{ width: `${p.widthPct}%` }} />
        ))}
      </div>
      <div className="legend">
        {parts.map((p) => {
          const color =
            p.k === "system"
              ? "var(--accent)"
              : p.k === "tools"
                ? "var(--violet)"
                : p.k === "log"
                  ? "var(--tone-ok)"
                  : "var(--tone-warn)";
          return (
            <div key={p.k}>
              <span className="sw" style={{ background: color }} />
              <span className="l">{p.label}</span>
              <span className="v">{p.value}</span>
            </div>
          );
        })}
      </div>
      {topTools.length > 0 ? (
        <div className="ttop">
          <div className="stt">Top tools — 占用</div>
          {topTools.map((t, i) => (
            <div className="row" key={i}>
              <span className="n">{t.name}</span>
              <div className="bbar">
                <span style={{ width: `${t.widthPct}%` }} />
              </div>
              <span className="v">{t.value}</span>
            </div>
          ))}
        </div>
      ) : null}
    </div>
  );
}

// ---- Memory groups ----

export type MemGroupKey = "user" | "feedback" | "project" | "reference";
export type MemEntry = { text: string; meta?: string };
export type MemGroups = Partial<Record<MemGroupKey, MemEntry[]>>;

const GROUP_LABELS: Record<MemGroupKey, string> = {
  user: "USER · 用户偏好",
  feedback: "FEEDBACK · 反馈与纠正",
  project: "PROJECT · 项目规范",
  reference: "REFERENCE · 引用资料",
};

export function MemoryGroups({ data }: { data: MemGroups }) {
  const keys: MemGroupKey[] = ["user", "feedback", "project", "reference"];
  return (
    <div className="mem-groups">
      {keys.map((g) => {
        const rows = data[g] ?? [];
        if (rows.length === 0) return null;
        return (
          <div key={g}>
            <div className="gh" data-g={g}>
              <span className="sw" />
              <span>{GROUP_LABELS[g]}</span>
              <span className="grow" />
              <span className="cnt">{rows.length}</span>
            </div>
            {rows.map((r, i) => (
              <div className="mrow" key={i}>
                <span className="b">·</span>
                <div className="t">{r.text}</div>
                {r.meta ? <span className="meta">{r.meta}</span> : null}
              </div>
            ))}
          </div>
        );
      })}
    </div>
  );
}

// ---- Fallback ----

export function FallbackCard({ kindLabel, payload }: { kindLabel: string; payload: Record<string, string> }) {
  return (
    <div className="fallback-card">
      <div className="hd"># unknown card kind — fallback render</div>
      <div className="kv">
        <span className="k">kind</span>
        <span className="v">"{kindLabel}"</span>
        {Object.entries(payload).map(([k, v]) => (
          <span key={k} style={{ display: "contents" }}>
            <span className="k">{k}</span>
            <span className="v">{v}</span>
          </span>
        ))}
      </div>
    </div>
  );
}

// ---- LiveCard (pill variants) ----

export type LiveVariant =
  | "thinking"
  | "undo"
  | "ctxPressure"
  | "aborted"
  | "retry"
  | "checkpoint"
  | "stepProgress"
  | "mcpEvent"
  | "sessionOp";

export function LiveCard({
  variant,
  icon,
  body,
  action,
  onAction,
}: {
  variant: LiveVariant;
  icon: ReactNode;
  body: ReactNode;
  action?: string;
  onAction?: () => void;
}) {
  return (
    <span className={`live-card ${variant === "stepProgress" ? "step" : ""}`} data-v={variant}>
      <span className="lc-ico">{icon}</span>
      <span className="lc-body">{body}</span>
      {action ? (
        <button type="button" className="lc-act" onClick={onAction}>
          {action}
        </button>
      ) : null}
    </span>
  );
}
