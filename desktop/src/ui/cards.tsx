import { memo, useState, type ReactNode } from "react";
import { I } from "../icons";
import { Markdown } from "../Markdown";
import { t, useLang } from "../i18n";
import { Shortcut } from "./shortcut";

type Tone = "default" | "success" | "warning" | "danger" | "accent" | "violet";

export function Card({
  tone = "default",
  icon,
  kind,
  name,
  meta,
  defaultOpen = true,
  compact = false,
  children,
  headRight,
}: {
  tone?: Tone;
  icon: ReactNode;
  kind: string;
  name?: ReactNode;
  meta?: ReactNode;
  defaultOpen?: boolean;
  /** Slimmer header — used for thinking / tool-call process cards so they
   *  read as background detail rather than primary content. */
  compact?: boolean;
  children: ReactNode;
  headRight?: ReactNode;
}) {
  const [open, setOpen] = useState(defaultOpen);
  return (
    <div className={compact ? "card is-compact" : "card"} data-tone={tone} data-open={open}>
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

function derivePlanBadge(items: PlanItem[]): { state: "running" | "done" | "failed" | "waiting" | "blocked"; label: string } {
  if (items.some((x) => x.status === "failed")) return { state: "failed", label: t("planBadge.failed") };
  if (items.some((x) => x.status === "blocked")) return { state: "blocked", label: t("planBadge.blocked") };
  if (items.some((x) => x.status === "active")) return { state: "running", label: t("planBadge.running") };
  if (items.length > 0 && items.every((x) => x.status === "done")) return { state: "done", label: t("planBadge.done") };
  return { state: "waiting", label: t("planBadge.pending") };
}

function StatusIcon({ state, label }: { state: "running" | "done" | "failed" | "waiting" | "blocked"; label: string }) {
  switch (state) {
    case "running":
      return <span className="spin-meta" role="img" aria-label={label} title={label} />;
    case "done":
      return <I.check size={10} style={{ color: "var(--success)" }} aria-label={label} />;
    case "failed":
      return <I.x size={10} style={{ color: "var(--danger)" }} aria-label={label} />;
    case "waiting":
      return <span className="status-dot warn" role="img" aria-label={label} title={label} />;
    case "blocked":
      return <I.slash size={10} style={{ color: "var(--warning)" }} aria-label={label} />;
  }
}

export function PlanCardView({ items, title }: { items: PlanItem[]; title?: string }) {
  useLang();
  const resolvedTitle = title ?? t("cards.planDefaultTitle");
  const done = items.filter((x) => x.status === "done").length;
  const badge = derivePlanBadge(items);
  return (
    <Card
      tone="accent"
      icon={<I.list size={12} />}
      kind="plan"
      name={resolvedTitle}
      meta={
        <>
          <span>
            {done}/{items.length}
          </span>
          <StatusIcon state={badge.state} label={badge.label} />
          <span className="meta-label">{badge.label}</span>
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
  useLang();
  return (
    <Card
      tone="violet"
      icon={<I.brain size={12} />}
      kind="reasoning"
      name={t("cards.reasoningName")}
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
            <StatusIcon state="running" label={t("cards.streaming")} />
          ) : (
            <StatusIcon state="done" label={t("cards.reasoningComplete")} />
          )}
        </>
      }
      defaultOpen={streaming}
      compact
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
                <span className="k">{t("settings.model")}</span> {model}
              </span>
            ) : null}
            {tokens !== undefined ? (
              <span>
                <span className="k">{t("statusbar.tokens")}</span> {tokens.toLocaleString()}
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
  defaultOpen,
}: {
  command: string;
  output?: string;
  state: "await" | "running" | "done" | "failed";
  durationMs?: number;
  onApprove?: () => void;
  onReject?: () => void;
  onAlwaysAllow?: () => void;
  defaultOpen?: boolean;
}) {
  useLang();
  const tone: Tone = state === "failed" ? "danger" : state === "done" ? "success" : "warning";
  return (
    <Card
      tone={tone}
      icon={<I.terminal size={12} />}
      kind="shell"
      name="shell"
      compact
      defaultOpen={defaultOpen ?? false}
      meta={
        <>
          {state === "await" ? (
            <StatusIcon state="waiting" label={t("cards.shellAwaiting")} />
          ) : state === "running" ? (
            <StatusIcon state="running" label={t("cards.shellRunning")} />
          ) : state === "failed" ? (
            <StatusIcon state="failed" label={t("cards.failed")} />
          ) : (
            <StatusIcon state="done" label={t("cards.done")} />
          )}
          {(state === "done" || state === "failed") && durationMs ? (
            <span className="meta-dur">{(durationMs / 1000).toFixed(2)}s</span>
          ) : null}
        </>
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
              <b>{t("cards.shellAwaiting")}</b> — {t("cards.shellExecuteHint")}
            </div>
            <div className="actions">
              {onAlwaysAllow ? (
                <button type="button" className="btn ghost" onClick={onAlwaysAllow}>
                  {t("cards.shellAlwaysAllow")}
                </button>
              ) : null}
              {onReject ? (
                <button type="button" className="btn" onClick={onReject}>
                  {t("cards.shellReject")} <Shortcut keys={["mod", "."]} />
                </button>
              ) : null}
              <button type="button" className="btn primary" onClick={onApprove}>
                {t("cards.shellRun")} <Shortcut keys={["mod", "enter"]} />
              </button>
            </div>
          </div>
        ) : null}
      </div>
    </Card>
  );
}

// ---- Compaction ----

export function CompactionCard({ summary }: { summary: string }) {
  useLang();
  const charCount = summary.length;
  return (
    <Card
      tone="default"
      icon={<I.archive size={12} />}
      kind="compaction"
      name={t("cards.compactionName")}
      meta={<span>{t("cards.compactionMeta", { chars: charCount.toLocaleString() })}</span>}
      defaultOpen={false}
      compact
    >
      <div className="compaction-body">
        <Markdown source={summary} />
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
  defaultOpen,
}: {
  name: string;
  args?: string;
  result?: string;
  ok?: boolean;
  durationMs?: number;
  defaultOpen?: boolean;
}) {
  useLang();
  const running = result === undefined;
  const tone: Tone = running ? "default" : ok === false ? "danger" : "success";
  return (
    <Card
      tone={tone}
      icon={<I.wrench size={12} />}
      kind="tool"
      name={name}
      defaultOpen={defaultOpen ?? false}
      compact
      meta={
        <>
          {running ? (
            <StatusIcon state="running" label={t("cards.running")} />
          ) : ok === false ? (
            <StatusIcon state="failed" label={t("cards.error")} />
          ) : (
            <StatusIcon state="done" label={t("cards.done")} />
          )}
          {!running && durationMs !== undefined ? (
            <span className="meta-dur">{durationMs} ms</span>
          ) : null}
        </>
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
            <span className="k">{ok === false ? t("cards.error") : t("cards.result")}</span>
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

export function parseEditResult(text: string): { filename: string; lines: DiffLine[] }[] {
  const files: { filename: string; lines: DiffLine[] }[] = [];
  const lines = text.split("\n");

  let currentFilename = "";
  let currentLines: DiffLine[] = [];
  let hunkStartLeft = 0;
  let hunkStartRight = 0;
  let leftLine = 0;
  let rightLine = 0;

  const flush = () => {
    if (currentLines.length > 0) {
      files.push({ filename: currentFilename, lines: currentLines });
    }
    currentLines = [];
    hunkStartLeft = 0;
    hunkStartRight = 0;
    leftLine = 0;
    rightLine = 0;
  };

  for (let i = 0; i < lines.length; i++) {
    const line = lines[i]!;

    if (line.startsWith("edited ") || line.startsWith("multi_edit:")) {
      const m = line.match(/^edited\s+(.+?)\s+\(/);
      if (m) currentFilename = m[1]!;
      continue;
    }

    if (line.startsWith("# ")) {
      flush();
      currentFilename = line.slice(2);
      continue;
    }

    const hunkMatch = line.match(/^@@\s+-(\d+),(\d+)\s+\+(\d+),(\d+)\s+@@/);
    if (hunkMatch) {
      hunkStartLeft = Number(hunkMatch[1]!);
      hunkStartRight = Number(hunkMatch[3]!);
      leftLine = hunkStartLeft;
      rightLine = hunkStartRight;
      currentLines.push({ t: "hunk", s: line });
      continue;
    }

    if (line.startsWith("+") && !line.startsWith("+++")) {
      currentLines.push({ t: "add", r: rightLine, s: line.slice(1) });
      rightLine++;
    } else if (line.startsWith("-") && !line.startsWith("---")) {
      currentLines.push({ t: "rm", l: leftLine, s: line.slice(1) });
      leftLine++;
    } else if (line.startsWith(" ")) {
      currentLines.push({ t: "ctx", l: leftLine, r: rightLine, s: line.slice(1) });
      leftLine++;
      rightLine++;
    }
  }

  flush();
  return files;
}

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
  useLang();
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
          {applied ? (
            <StatusIcon state="done" label={t("cards.applied")} />
          ) : (
            <StatusIcon state="waiting" label={t("cards.diffAwaiting")} />
          )}
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
              <b>{t("cards.diffApplyChanges")}</b> · +{adds} / −{rms}
            </div>
            <div className="actions">
              {onDiscard ? (
                <button type="button" className="btn" onClick={onDiscard}>
                  {t("cards.diffDiscard")}
                </button>
              ) : null}
              {onApply ? (
                <button type="button" className="btn primary" onClick={onApply}>
                  {t("cards.diffApply")} <Shortcut keys={["mod", "enter"]} />
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
  useLang();
  return (
    <Card
      tone="danger"
      icon={<I.warning size={12} />}
      kind="error"
      name={t("cards.errorName")}
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
  useLang();
  return (
    <Card
      tone="default"
      icon={<I.globe size={12} />}
      kind="web_search"
      name={t("cards.searchName")}
      meta={
        <>
          <span>"{query}"</span>
          <span className="pill-tag ok">{results.length} {t("cards.hits")}</span>
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
  useLang();
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
            {done} / {children.length} {t("cards.subagentDoneProgress")}
          </span>
          {status === "done" ? (
            <StatusIcon state="done" label={t("cards.subagentDone")} />
          ) : status === "failed" ? (
            <StatusIcon state="failed" label={t("cards.subagentFailed")} />
          ) : (
            <StatusIcon state="running" label={t("cards.subagentRunning")} />
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
  useLang();
  return (
    <Card
      tone="violet"
      icon={<I.bookmark size={12} />}
      kind="memory"
      name={t("cards.memoryName")}
      meta={<span>+ {rows.length} {t("cards.memoryCountSuffix")}</span>}
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
  useLang();
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
          <span>{t("cards.cacheHit")}</span>
          <span className="v acc">{cacheHit}%</span>
        </span>
      ) : null}
      {promptTokens !== undefined ? (
        <span className="item">
          <span>{t("cards.prompt")}</span>
          <span className="v">{promptTokens.toLocaleString()} t</span>
        </span>
      ) : null}
      {outputTokens !== undefined ? (
        <span className="item">
          <span>{t("cards.output")}</span>
          <span className="v">{outputTokens.toLocaleString()} t</span>
        </span>
      ) : null}
      {costLabel ? (
        <span className="item">
          <I.coin size={11} />
          <span>{t("cards.cost")}</span>
          <span className="v ok">{costLabel}</span>
        </span>
      ) : null}
      {elapsed ? (
        <span className="item">
          <span>{t("cards.elapsed")}</span>
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
  useLang();
  return (
    <div className="checkpoint">
      <I.history size={12} />
      <span className="hash">{hash}</span>
      <span>·</span>
      <span>{label}</span>
      {onRewind ? (
        <button type="button" onClick={onRewind}>
          {t("cards.checkpointRewind")}
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
