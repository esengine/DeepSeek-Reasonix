import { memo, useState, type ReactNode } from "react";
import { I } from "../icons";
import { Markdown } from "../Markdown";

type Tone = "default" | "success" | "warning" | "danger" | "accent" | "violet";

export function Card({
  tone = "default",
  icon,
  kind,
  name,
  meta,
  defaultOpen = true,
  children,
  headRight,
}: {
  tone?: Tone;
  icon: ReactNode;
  kind: string;
  name?: ReactNode;
  meta?: ReactNode;
  defaultOpen?: boolean;
  children: ReactNode;
  headRight?: ReactNode;
}) {
  const [open, setOpen] = useState(defaultOpen);
  return (
    <div className="card" data-tone={tone} data-open={open}>
      <button
        type="button"
        className="card-head"
        onClick={() => setOpen((v) => !v)}
        style={{
          width: "100%",
          background: "none",
          border: "none",
          textAlign: "left",
          font: "inherit",
          color: "inherit",
        }}
      >
        <span className="ico">{icon}</span>
        <span className="kind">{kind}</span>
        {name ? <span className="name">{name}</span> : null}
        <span className="grow" />
        {meta ? <span className="meta">{meta}</span> : null}
        {headRight}
        <span className="chev">
          <I.chev size={12} />
        </span>
      </button>
      {open ? <div className="card-body">{children}</div> : null}
    </div>
  );
}

// ---- Plan ----

export type PlanItem = {
  id: string | number;
  status: "todo" | "active" | "done" | "failed" | "blocked" | "skipped";
  text: string;
  tool?: string;
  note?: string;
};

function derivePlanBadge(items: PlanItem[]): { cls: "run" | "ok" | "warn" | "err"; label: string } {
  if (items.some((x) => x.status === "failed")) return { cls: "err", label: "失败" };
  if (items.some((x) => x.status === "blocked")) return { cls: "warn", label: "阻塞" };
  if (items.some((x) => x.status === "active")) return { cls: "run", label: "运行中" };
  if (items.length > 0 && items.every((x) => x.status === "done")) return { cls: "ok", label: "已完成" };
  return { cls: "run", label: "待开始" };
}

export function PlanCardView({ items, title = "计划" }: { items: PlanItem[]; title?: string }) {
  const done = items.filter((x) => x.status === "done").length;
  const badge = derivePlanBadge(items);
  return (
    <Card
      tone="accent"
      icon={<I.list size={12} />}
      kind="plan"
      name={title}
      meta={
        <>
          <span>
            {done}/{items.length}
          </span>
          <span className={`pill-tag ${badge.cls}`}>{badge.label}</span>
        </>
      }
    >
      <ul className="plan-list" style={{ listStyle: "none", margin: 0, padding: "8px 12px 12px" }}>
        {items.map((it) => (
          <li key={it.id} className="plan-item" data-status={it.status}>
            <span className="ck">{it.status === "done" ? <I.check size={12} /> : null}</span>
            <div>
              <div className="text">{it.text}</div>
              {it.tool || it.note ? (
                <div className="sub">
                  {it.tool ? <span className="tool">{it.tool}</span> : null}
                  {it.note ? <span>{it.note}</span> : null}
                </div>
              ) : null}
            </div>
            <span className="stat">{it.status === "active" ? <span className="spin" /> : null}</span>
          </li>
        ))}
      </ul>
    </Card>
  );
}

// ---- Reasoning ----

export function ReasoningCard({
  text,
  streaming,
  tokens,
  elapsed,
  model,
}: {
  text: string;
  streaming: boolean;
  tokens?: number;
  elapsed?: string;
  model?: string;
}) {
  return (
    <Card
      tone="violet"
      icon={<I.brain size={12} />}
      kind="reasoning"
      name="思考"
      meta={
        <>
          {elapsed || tokens ? (
            <span>
              {elapsed ?? ""}
              {elapsed && tokens ? " · " : ""}
              {tokens ? `${tokens.toLocaleString()} t` : ""}
            </span>
          ) : null}
          {streaming ? (
            <span className="pill-tag warn">
              <span className="shimmer">streaming…</span>
            </span>
          ) : (
            <span className="pill-tag ok">完成</span>
          )}
        </>
      }
      defaultOpen={streaming}
    >
      <div className="reason">
        <div className="stream">
          {text.split(/\n\n+/).map((para, i) => (
            <p
              key={i}
              dangerouslySetInnerHTML={{
                __html: para
                  .replace(/`([^`]+)`/g, '<span class="hl">$1</span>')
                  .replace(/\*\*([^*]+)\*\*/g, "<strong>$1</strong>"),
              }}
            />
          ))}
        </div>
        {model || tokens !== undefined ? (
          <div className="meta">
            {model ? (
              <span>
                <span className="k">model</span> {model}
              </span>
            ) : null}
            {tokens !== undefined ? (
              <span>
                <span className="k">tokens</span> {tokens.toLocaleString()}
              </span>
            ) : null}
          </div>
        ) : null}
      </div>
    </Card>
  );
}

// ---- Shell ----

export function ShellCard({
  command,
  output,
  state,
  durationMs,
  onApprove,
  onReject,
  onAlwaysAllow,
}: {
  command: string;
  output?: string;
  state: "await" | "running" | "done" | "failed";
  durationMs?: number;
  onApprove?: () => void;
  onReject?: () => void;
  onAlwaysAllow?: () => void;
}) {
  const tone: Tone = state === "failed" ? "danger" : state === "done" ? "success" : "warning";
  const durationLabel = durationMs ? ` · ${(durationMs / 1000).toFixed(2)}s` : "";
  return (
    <Card
      tone={tone}
      icon={<I.terminal size={12} />}
      kind="shell"
      name="shell"
      meta={
        state === "await" ? (
          <span className="pill-tag warn">等待批准</span>
        ) : state === "running" ? (
          <span className="pill-tag run">运行中</span>
        ) : state === "failed" ? (
          <span className="pill-tag err">failed{durationLabel}</span>
        ) : (
          <span className="pill-tag ok">done{durationLabel}</span>
        )
      }
    >
      <div className="shell">
        <div className="cmd">
          <span className="prompt">$</span>
          <span className="text">{command}</span>
        </div>
        {output ? (
          <pre className="out">
            {output.split("\n").map((ln, i) => {
              if (ln.startsWith(" ✓") || ln.startsWith("✓"))
                return (
                  <div key={i}>
                    <span className="ok">{ln}</span>
                  </div>
                );
              if (ln.startsWith(" ✗") || ln.startsWith("✗") || /error/i.test(ln))
                return (
                  <div key={i}>
                    <span className="err">{ln}</span>
                  </div>
                );
              return <div key={i}>{ln}</div>;
            })}
          </pre>
        ) : null}
        {state === "await" && onApprove ? (
          <div className="approve-row">
            <div className="why">
              <b>等待批准</b> — 执行此命令
            </div>
            <div className="actions">
              {onAlwaysAllow ? (
                <button type="button" className="btn ghost" onClick={onAlwaysAllow}>
                  始终允许
                </button>
              ) : null}
              {onReject ? (
                <button type="button" className="btn" onClick={onReject}>
                  拒绝 <kbd>⌘.</kbd>
                </button>
              ) : null}
              <button type="button" className="btn primary" onClick={onApprove}>
                运行 <kbd>⌘⏎</kbd>
              </button>
            </div>
          </div>
        ) : null}
      </div>
    </Card>
  );
}

// ---- Generic Tool ----

export function ToolCard({
  name,
  args,
  result,
  ok,
  durationMs,
}: {
  name: string;
  args?: string;
  result?: string;
  ok?: boolean;
  durationMs?: number;
}) {
  const running = result === undefined;
  const tone: Tone = running ? "default" : ok === false ? "danger" : "success";
  const dur = durationMs ? `${durationMs} ms` : "—";
  return (
    <Card
      tone={tone}
      icon={<I.wrench size={12} />}
      kind="tool"
      name={name}
      defaultOpen={false}
      meta={
        running ? (
          <span className="pill-tag run">running</span>
        ) : ok === false ? (
          <span className="pill-tag err">error · {dur}</span>
        ) : (
          <span className="pill-tag ok">done · {dur}</span>
        )
      }
    >
      <div className="tool-call">
        {args ? (
          <div className="row">
            <span className="k">args</span>
            <span className="v">
              <span className="str">{args.length > 600 ? `${args.slice(0, 600)}…` : args}</span>
            </span>
          </div>
        ) : null}
        {result !== undefined ? (
          <div className="row">
            <span className="k">{ok === false ? "error" : "result"}</span>
            <span className="v">
              <span className={ok === false ? "num" : "str"}>
                {result.length > 1200 ? `${result.slice(0, 1200)}…` : result}
              </span>
            </span>
          </div>
        ) : null}
      </div>
    </Card>
  );
}

// ---- Diff ----

export type DiffLine =
  | { t: "hunk"; s: string }
  | { t: "ctx"; l?: number; r?: number; s: string }
  | { t: "add"; r: number; s: string }
  | { t: "rm"; l: number; s: string };

export function DiffCard({
  filename,
  lines,
  applied,
  onApply,
  onDiscard,
}: {
  filename: string;
  lines: DiffLine[];
  applied?: boolean;
  onApply?: () => void;
  onDiscard?: () => void;
}) {
  const adds = lines.filter((x) => x.t === "add").length;
  const rms = lines.filter((x) => x.t === "rm").length;
  return (
    <Card
      tone={applied ? "success" : "accent"}
      icon={<I.diff size={12} />}
      kind="edit"
      name={filename}
      meta={
        <>
          <span style={{ color: "var(--success)" }}>+{adds}</span>
          <span style={{ color: "var(--danger)" }}>−{rms}</span>
          {applied ? <span className="pill-tag ok">applied</span> : <span className="pill-tag warn">等待批准</span>}
        </>
      }
    >
      <div className="diff">
        <div className="lines">
          {lines.map((ln, i) => {
            if (ln.t === "hunk")
              return (
                <div key={i} className="ln hunk">
                  <span className="code">{ln.s}</span>
                </div>
              );
            const cls = ln.t === "add" ? "add" : ln.t === "rm" ? "rm" : "";
            const l = ln.t === "ctx" ? ln.l : ln.t === "rm" ? ln.l : undefined;
            const r = ln.t === "ctx" ? ln.r : ln.t === "add" ? ln.r : undefined;
            return (
              <div key={i} className={`ln ${cls}`}>
                <span className="num">{l ?? ""}</span>
                <span className="num">{r ?? ""}</span>
                <span className="code">
                  {ln.t === "add" ? "+ " : ln.t === "rm" ? "− " : "  "}
                  {ln.s}
                </span>
              </div>
            );
          })}
        </div>
        {!applied && (onApply || onDiscard) ? (
          <div className="approve-row">
            <div className="why">
              <b>应用变更</b> · +{adds} / −{rms}
            </div>
            <div className="actions">
              {onDiscard ? (
                <button type="button" className="btn" onClick={onDiscard}>
                  丢弃
                </button>
              ) : null}
              {onApply ? (
                <button type="button" className="btn primary" onClick={onApply}>
                  应用 <kbd>⌘⏎</kbd>
                </button>
              ) : null}
            </div>
          </div>
        ) : null}
      </div>
    </Card>
  );
}

// ---- Error ----

export function ErrorCard({ message, hint, code }: { message: string; hint?: ReactNode; code?: string }) {
  return (
    <Card
      tone="danger"
      icon={<I.warning size={12} />}
      kind="error"
      name="错误"
      meta={code ? <span className="pill-tag err">{code}</span> : null}
    >
      <div className="error-body">
        <div className="msg-err">{message}</div>
        {hint ? <div className="hint">{hint}</div> : null}
      </div>
    </Card>
  );
}

// ---- Search results ----

export type SearchHit = { url: string; title: string; snippet: string };

export function WebSearchCard({ query, results }: { query: string; results: SearchHit[] }) {
  return (
    <Card
      tone="default"
      icon={<I.globe size={12} />}
      kind="web_search"
      name="网页搜索"
      meta={
        <>
          <span>"{query}"</span>
          <span className="pill-tag ok">{results.length} hits</span>
        </>
      }
    >
      <div className="search-results">
        {results.map((r, i) => (
          <div className="search-result" key={i}>
            <div className="url">
              <span className="favicon" />
              <span>{r.url}</span>
            </div>
            <div className="title">{r.title}</div>
            <div className="snippet">{r.snippet}</div>
          </div>
        ))}
      </div>
    </Card>
  );
}

// ---- Subagent ----

export type SubAgentChild = {
  avatar: string;
  what: string;
  role: string;
  status: "done" | "running" | "queued";
};

export function SubagentCard({
  name,
  children,
  status,
}: {
  name: string;
  children: SubAgentChild[];
  status: "running" | "done" | "failed";
}) {
  const done = children.filter((c) => c.status === "done").length;
  return (
    <Card
      tone="violet"
      icon={<I.bot size={12} />}
      kind="subagent"
      name={name}
      meta={
        <>
          <span>
            {done} / {children.length} 完成
          </span>
          {status === "done" ? (
            <span className="pill-tag ok">完成</span>
          ) : status === "failed" ? (
            <span className="pill-tag err">失败</span>
          ) : (
            <span className="pill-tag run">运行中</span>
          )}
        </>
      }
    >
      <div className="sub-card">
        {children.map((c, i) => (
          <div className="sub-row" key={i}>
            <span className="av">{c.avatar}</span>
            <div className="what">
              <div>{c.what}</div>
              <div className="role">{c.role}</div>
            </div>
            <span className="prog">
              {c.status === "done" ? (
                <I.check size={12} style={{ color: "var(--success)" }} />
              ) : c.status === "running" ? (
                <span className="spin" />
              ) : null}
            </span>
          </div>
        ))}
      </div>
    </Card>
  );
}

// ---- Memory rows ----

export type MemRow = { scope: string; txt: string };

export function MemoryCard({ rows }: { rows: MemRow[] }) {
  return (
    <Card
      tone="violet"
      icon={<I.bookmark size={12} />}
      kind="memory"
      name="记忆"
      meta={<span>+ {rows.length} 项</span>}
    >
      <div className="mem">
        {rows.map((m, i) => (
          <div className="mem-row" key={i}>
            <span className="scope">{m.scope}</span>
            <span className="txt">{m.txt}</span>
          </div>
        ))}
      </div>
    </Card>
  );
}

// ---- Image attachment ----

export function AttachCard({
  filename,
  meta,
  preview,
}: {
  filename: string;
  meta: string;
  preview?: string;
}) {
  return (
    <Card
      tone="default"
      icon={<I.image size={12} />}
      kind="image"
      name={filename}
      meta={<span>{meta}</span>}
    >
      <div className="attach-card">
        <div className="ph">{preview ?? "PNG"}</div>
        <div className="info">
          <div className="n">{filename}</div>
          <div className="m">{meta}</div>
        </div>
        <button type="button" className="btn ghost">
          <I.download size={12} />
        </button>
      </div>
    </Card>
  );
}

// ---- Metric strip (inline) ----

export function MetricStrip({
  cacheHit,
  promptTokens,
  outputTokens,
  costLabel,
  elapsed,
}: {
  cacheHit?: number;
  promptTokens?: number;
  outputTokens?: number;
  costLabel?: string;
  elapsed?: string;
}) {
  return (
    <div className="metric-strip">
      {cacheHit !== undefined ? (
        <span className="item">
          <I.zap size={11} style={{ color: "var(--accent)" }} />
          <span>cache_hit</span>
          <span className="v acc">{cacheHit}%</span>
        </span>
      ) : null}
      {promptTokens !== undefined ? (
        <span className="item">
          <span>prompt</span>
          <span className="v">{promptTokens.toLocaleString()} t</span>
        </span>
      ) : null}
      {outputTokens !== undefined ? (
        <span className="item">
          <span>output</span>
          <span className="v">{outputTokens.toLocaleString()} t</span>
        </span>
      ) : null}
      {costLabel ? (
        <span className="item">
          <I.coin size={11} />
          <span>cost</span>
          <span className="v ok">{costLabel}</span>
        </span>
      ) : null}
      {elapsed ? (
        <span className="item">
          <span>elapsed</span>
          <span className="v">{elapsed}</span>
        </span>
      ) : null}
    </div>
  );
}

// ---- Checkpoint marker (inline) ----

export function Checkpoint({
  hash,
  label,
  onRewind,
}: {
  hash: string;
  label: string;
  onRewind?: () => void;
}) {
  return (
    <div className="checkpoint">
      <I.history size={12} />
      <span className="hash">{hash}</span>
      <span>·</span>
      <span>{label}</span>
      {onRewind ? (
        <button type="button" onClick={onRewind}>
          回到此处
        </button>
      ) : null}
    </div>
  );
}

// ---- Plain text block (assistant content via markdown) ----

export const AssistantText = memo(function AssistantText({ text }: { text: string }) {
  return (
    <div className="msg-text">
      <Markdown source={text} />
    </div>
  );
});
