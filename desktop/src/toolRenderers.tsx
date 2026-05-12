import {
  Brain,
  CheckSquare,
  ClipboardList,
  Code,
  FileText,
  FolderOpen,
  GitBranch,
  Pencil,
  Plus,
  Search,
  Sparkles,
  Telescope,
  Terminal,
  Wrench,
} from "lucide-react";
import { Highlight } from "prism-react-renderer";
import { type ReactNode, useState } from "react";
import { CollapsibleCode, langFromPath, PRISM_THEME } from "./CodeView";

export type ToolKind = "read" | "write" | "exec" | "meta" | "unknown";

type ToolDef = {
  kind: ToolKind;
  icon: ReactNode;
  /** Pick the most informative field from parsed args to show in the header. */
  preview?: (args: ParsedArgs) => string | null;
};

type ParsedArgs = Record<string, unknown>;

const sz = 12;

const TOOLS: Record<string, ToolDef> = {
  read_file: {
    kind: "read",
    icon: <FileText size={sz} />,
    preview: (a) => str(a.path) ?? str(a.file_path),
  },
  ls: {
    kind: "read",
    icon: <FolderOpen size={sz} />,
    preview: (a) => str(a.path) ?? ".",
  },
  glob_files: {
    kind: "read",
    icon: <Search size={sz} />,
    preview: (a) => str(a.pattern) ?? str(a.glob),
  },
  grep_files: {
    kind: "read",
    icon: <Search size={sz} />,
    preview: (a) => str(a.pattern),
  },
  semantic_search: {
    kind: "read",
    icon: <Telescope size={sz} />,
    preview: (a) => str(a.query),
  },
  edit_file: {
    kind: "write",
    icon: <Pencil size={sz} />,
    preview: (a) => str(a.path) ?? str(a.file_path),
  },
  multi_edit: {
    kind: "write",
    icon: <Pencil size={sz} />,
    preview: (a) => str(a.path) ?? str(a.file_path),
  },
  write_file: {
    kind: "write",
    icon: <Plus size={sz} />,
    preview: (a) => str(a.path) ?? str(a.file_path),
  },
  run_command: {
    kind: "exec",
    icon: <Terminal size={sz} />,
    preview: (a) => str(a.command),
  },
  run_background_command: {
    kind: "exec",
    icon: <Terminal size={sz} />,
    preview: (a) => str(a.command),
  },
  remember: {
    kind: "meta",
    icon: <Brain size={sz} />,
    preview: (a) => str(a.scope) ?? str(a.key),
  },
  forget: {
    kind: "meta",
    icon: <Brain size={sz} />,
    preview: (a) => str(a.key),
  },
  recall_memory: {
    kind: "meta",
    icon: <Brain size={sz} />,
    preview: (a) => str(a.query) ?? str(a.scope),
  },
  submit_plan: {
    kind: "meta",
    icon: <ClipboardList size={sz} />,
    preview: (a) => {
      const steps = a.steps;
      return Array.isArray(steps) ? `${steps.length} steps` : null;
    },
  },
  todo_write: {
    kind: "meta",
    icon: <CheckSquare size={sz} />,
    preview: (a) => {
      const items = a.items ?? a.todos;
      return Array.isArray(items) ? `${items.length} items` : null;
    },
  },
  ask_choice: {
    kind: "meta",
    icon: <GitBranch size={sz} />,
    preview: (a) => str(a.question),
  },
  create_skill: {
    kind: "meta",
    icon: <Sparkles size={sz} />,
    preview: (a) => str(a.name),
  },
  add_mcp_server: {
    kind: "meta",
    icon: <Wrench size={sz} />,
    preview: (a) => str(a.name),
  },
};

const FALLBACK: ToolDef = { kind: "unknown", icon: <Code size={sz} /> };

function str(v: unknown): string | null {
  return typeof v === "string" && v.trim() ? v : null;
}

export function getToolDef(name: string): ToolDef {
  return TOOLS[name] ?? FALLBACK;
}

function parseArgsSafe(args: string): ParsedArgs | null {
  if (!args) return null;
  try {
    const p = JSON.parse(args);
    return p && typeof p === "object" ? (p as ParsedArgs) : null;
  } catch {
    return null;
  }
}

export function previewFor(name: string, args: string): string | null {
  const def = getToolDef(name);
  const parsed = parseArgsSafe(args);
  if (parsed && def.preview) return def.preview(parsed);
  return args.replace(/\s+/g, " ").slice(0, 100) || null;
}

/** One-glance summary of the result — shown next to the row header when collapsed. */
export function summaryFor(
  name: string,
  args: string,
  result: string | undefined,
  ok: boolean | undefined,
): { text: string; tone: "ok" | "warn" | "neutral" } | null {
  if (result === undefined) return null;
  if (ok === false) return { text: "failed", tone: "warn" };

  switch (name) {
    case "read_file": {
      const lines = result.split("\n").length;
      return { text: `${lines} ${lines === 1 ? "line" : "lines"}`, tone: "neutral" };
    }
    case "grep_files": {
      const matches = result.split("\n").filter((l) => l.length > 0).length;
      return { text: `${matches} ${matches === 1 ? "match" : "matches"}`, tone: "neutral" };
    }
    case "glob_files":
    case "ls": {
      const entries = result.split("\n").filter((l) => l.length > 0).length;
      return { text: `${entries} ${entries === 1 ? "entry" : "entries"}`, tone: "neutral" };
    }
    case "semantic_search": {
      const blocks = result.split("\n\n").filter((b) => b.trim().length > 0).length;
      return { text: `${blocks} hit${blocks === 1 ? "" : "s"}`, tone: "neutral" };
    }
    case "edit_file": {
      const parsed = parseArgsSafe(args);
      const raw = str(parsed?.edit) ?? str(parsed?.search_replace) ?? args;
      const sr = parseSR(raw);
      if (!sr) return { text: "edited", tone: "ok" };
      return {
        text: `+${sr.replace.split("\n").length} −${sr.search.split("\n").length}`,
        tone: "ok",
      };
    }
    case "multi_edit": {
      const parsed = parseArgsSafe(args);
      const edits = Array.isArray(parsed?.edits) ? parsed.edits : [];
      let added = 0;
      let removed = 0;
      for (const e of edits) {
        const raw =
          typeof e === "string"
            ? e
            : str((e as ParsedArgs).search_replace) ?? str((e as ParsedArgs).edit);
        const sr = parseSR(raw);
        if (sr) {
          added += sr.replace.split("\n").length;
          removed += sr.search.split("\n").length;
        }
      }
      return added || removed
        ? { text: `+${added} −${removed}`, tone: "ok" }
        : { text: `${edits.length} edits`, tone: "ok" };
    }
    case "write_file": {
      return { text: "written", tone: "ok" };
    }
    case "run_command":
    case "run_background_command": {
      const lines = result.split("\n").filter((l) => l.length > 0).length;
      if (lines === 0) return { text: "no output", tone: "neutral" };
      return { text: `${lines} ${lines === 1 ? "line" : "lines"} out`, tone: "neutral" };
    }
    case "submit_plan": {
      const parsed = parseArgsSafe(args);
      const steps = Array.isArray(parsed?.steps) ? parsed.steps.length : 0;
      return { text: `${steps} step${steps === 1 ? "" : "s"}`, tone: "neutral" };
    }
    case "todo_write": {
      const parsed = parseArgsSafe(args);
      const items = (parsed?.items ?? parsed?.todos) as unknown;
      const count = Array.isArray(items) ? items.length : 0;
      return { text: `${count} item${count === 1 ? "" : "s"}`, tone: "neutral" };
    }
    default:
      return null;
  }
}

function parseSR(raw: string | null): { search: string; replace: string } | null {
  if (!raw) return null;
  const sep = raw.indexOf("\n=======\n");
  if (sep < 0) return null;
  return { search: raw.slice(0, sep), replace: raw.slice(sep + "\n=======\n".length) };
}

/** Custom body — returns null to indicate "use generic args+result rendering". */
export function renderToolBody(
  name: string,
  args: string,
  result: string | undefined,
  ok: boolean | undefined,
): ReactNode | null {
  const parsed = parseArgsSafe(args);
  switch (name) {
    case "run_command":
    case "run_background_command":
      return <RunCommandBody command={str(parsed?.command)} result={result} ok={ok} />;
    case "edit_file":
      return <EditFileBody parsed={parsed} args={args} result={result} ok={ok} />;
    case "multi_edit":
      return <MultiEditBody parsed={parsed} args={args} result={result} ok={ok} />;
    case "read_file":
      return <ReadFileBody parsed={parsed} result={result} ok={ok} />;
    case "grep_files":
      return <GrepBody parsed={parsed} result={result} ok={ok} />;
    case "glob_files":
    case "ls":
      return <ListBody parsed={parsed} result={result} ok={ok} />;
    case "todo_write":
      return <TodoBody parsed={parsed} result={result} ok={ok} />;
    case "submit_plan":
      return <PlanBody parsed={parsed} result={result} ok={ok} />;
    case "ask_choice":
      return <ChoiceBody parsed={parsed} result={result} ok={ok} />;
    case "remember":
    case "forget":
    case "recall_memory":
      return <MemoryBody parsed={parsed} result={result} ok={ok} />;
    default:
      return null;
  }
}

/* ─── shared section primitives ─── */
function Section({
  label,
  children,
  mono = true,
}: {
  label: string;
  children: ReactNode;
  mono?: boolean;
}) {
  return (
    <div className="tool-section">
      <div className="tool-section-label">{label}</div>
      <div className={mono ? "tool-mono" : ""}>{children}</div>
    </div>
  );
}

function CollapsibleOutput({
  text,
  emptyHint = "(empty)",
  maxLines = 14,
}: {
  text: string;
  emptyHint?: string;
  maxLines?: number;
}) {
  const [open, setOpen] = useState(false);
  const lines = text.split("\n");
  const tooLong = lines.length > maxLines;
  const shown = open || !tooLong ? text : lines.slice(0, maxLines).join("\n");
  if (!text) return <span style={{ color: "var(--text-4)" }}>{emptyHint}</span>;
  return (
    <>
      <div className="tool-pre">{shown}</div>
      {tooLong && (
        <button type="button" className="tool-more" onClick={() => setOpen((v) => !v)}>
          {open ? "less" : `+ ${lines.length - maxLines} more lines`}
        </button>
      )}
    </>
  );
}

/* ─── run_command ─── */
function RunCommandBody({
  command,
  result,
  ok,
}: {
  command: string | null;
  result: string | undefined;
  ok: boolean | undefined;
}) {
  return (
    <>
      {command && (
        <Section label="command">
          <span className="tool-prompt">$</span> {command}
        </Section>
      )}
      {result !== undefined && (
        <Section label={ok === false ? "error" : "output"}>
          <CollapsibleOutput text={result} />
        </Section>
      )}
    </>
  );
}

/* ─── edit_file ─── */
function parseSearchReplace(raw: string | null): { search: string; replace: string } | null {
  if (!raw) return null;
  const sep = raw.indexOf("\n=======\n");
  if (sep < 0) return null;
  return {
    search: raw.slice(0, sep),
    replace: raw.slice(sep + "\n=======\n".length),
  };
}

function diffStat(parts: { search: string; replace: string }[]): { added: number; removed: number } {
  let added = 0;
  let removed = 0;
  for (const p of parts) {
    removed += p.search.split("\n").length;
    added += p.replace.split("\n").length;
  }
  return { added, removed };
}

function DiffSide({
  text,
  kind,
  lang,
}: {
  text: string;
  kind: "del" | "add";
  lang: string | null;
}) {
  if (!lang) {
    return (
      <>
        {text.split("\n").map((l, i) => (
          <div key={i} className={`tool-diff-line ${kind}`}>
            <span className="tool-diff-mark">{kind === "add" ? "+" : "-"}</span>
            {l || " "}
          </div>
        ))}
      </>
    );
  }
  return (
    <Highlight theme={PRISM_THEME} code={text} language={lang}>
      {({ tokens, getTokenProps }) => (
        <>
          {tokens.map((line, i) => (
            <div key={i} className={`tool-diff-line ${kind}`}>
              <span className="tool-diff-mark">{kind === "add" ? "+" : "-"}</span>
              {line.length === 0 ? (
                <span>{" "}</span>
              ) : (
                line.map((token, k) => <span key={k} {...getTokenProps({ token })} />)
              )}
            </div>
          ))}
        </>
      )}
    </Highlight>
  );
}

function DiffLines({
  search,
  replace,
  lang,
}: {
  search: string;
  replace: string;
  lang?: string | null;
}) {
  return (
    <div className="tool-diff">
      <DiffSide text={search} kind="del" lang={lang ?? null} />
      <DiffSide text={replace} kind="add" lang={lang ?? null} />
    </div>
  );
}

function EditFileBody({
  parsed,
  args,
  result,
  ok,
}: {
  parsed: ParsedArgs | null;
  args: string;
  result: string | undefined;
  ok: boolean | undefined;
}) {
  const sr = parseSearchReplace(str(parsed?.edit) ?? str(parsed?.search_replace) ?? args);
  const path = str(parsed?.path) ?? str(parsed?.file_path);
  const lang = langFromPath(path);
  return (
    <>
      {path && <Section label="file" mono>{path}</Section>}
      {sr ? (
        <Section label="diff">
          <DiffLines search={sr.search} replace={sr.replace} lang={lang} />
        </Section>
      ) : (
        <Section label="arguments">{args}</Section>
      )}
      {result !== undefined && (
        <Section label={ok === false ? "error" : "result"}>
          <CollapsibleOutput text={result} maxLines={8} />
        </Section>
      )}
    </>
  );
}

/* ─── multi_edit ─── */
function MultiEditBody({
  parsed,
  args,
  result,
  ok,
}: {
  parsed: ParsedArgs | null;
  args: string;
  result: string | undefined;
  ok: boolean | undefined;
}) {
  const path = str(parsed?.path) ?? str(parsed?.file_path);
  const lang = langFromPath(path);
  const edits = Array.isArray(parsed?.edits) ? parsed.edits : null;
  const parts = edits
    ? edits
        .map((e) =>
          parseSearchReplace(
            typeof e === "string" ? e : str((e as ParsedArgs).search_replace) ?? str((e as ParsedArgs).edit),
          ),
        )
        .filter((p): p is { search: string; replace: string } => p !== null)
    : [];
  const stat = parts.length > 0 ? diffStat(parts) : null;
  return (
    <>
      {path && (
        <Section label="file" mono>
          {path}
          {stat && (
            <span style={{ marginLeft: 10, color: "var(--text-3)", fontSize: 11 }}>
              <span style={{ color: "var(--success)" }}>+{stat.added}</span>{" "}
              <span style={{ color: "var(--danger)" }}>−{stat.removed}</span>
            </span>
          )}
        </Section>
      )}
      {parts.length > 0 ? (
        parts.map((p, i) => (
          <Section key={i} label={`edit ${i + 1} / ${parts.length}`}>
            <DiffLines search={p.search} replace={p.replace} lang={lang} />
          </Section>
        ))
      ) : (
        <Section label="arguments">{args}</Section>
      )}
      {result !== undefined && (
        <Section label={ok === false ? "error" : "result"}>
          <CollapsibleOutput text={result} maxLines={8} />
        </Section>
      )}
    </>
  );
}

/* ─── read_file ─── */
function ReadFileBody({
  parsed,
  result,
  ok,
}: {
  parsed: ParsedArgs | null;
  result: string | undefined;
  ok: boolean | undefined;
}) {
  const path = str(parsed?.path) ?? str(parsed?.file_path);
  const range = formatRange(parsed);
  const lang = langFromPath(path);
  const startRaw = parsed?.start_line ?? parsed?.startLine;
  const startLine = typeof startRaw === "number" && startRaw > 0 ? startRaw : 1;
  return (
    <>
      <Section label="file" mono>
        {path}
        {range && (
          <span style={{ marginLeft: 8, color: "var(--text-3)", fontSize: 11 }}>{range}</span>
        )}
      </Section>
      {result !== undefined && (
        <Section label={ok === false ? "error" : "content"} mono={!lang || ok === false}>
          {ok !== false && typeof result === "string" && (
            <div style={{ color: "var(--text-3)", fontSize: 10.5, marginBottom: 6 }}>
              {result.split("\n").length} lines · {result.length} chars
            </div>
          )}
          {lang && ok !== false ? (
            <CollapsibleCode text={result ?? ""} lang={lang} startLine={startLine} maxLines={20} />
          ) : (
            <CollapsibleOutput text={result ?? ""} maxLines={20} />
          )}
        </Section>
      )}
    </>
  );
}

function formatRange(args: ParsedArgs | null): string | null {
  if (!args) return null;
  const startLine = args.start_line ?? args.startLine;
  const endLine = args.end_line ?? args.endLine;
  if (typeof startLine === "number" && typeof endLine === "number") return `L${startLine}–L${endLine}`;
  if (typeof startLine === "number") return `from L${startLine}`;
  const head = args.head;
  const tail = args.tail;
  if (typeof head === "number") return `head ${head}`;
  if (typeof tail === "number") return `tail ${tail}`;
  return null;
}

/* ─── grep ─── */
function GrepBody({
  parsed,
  result,
  ok,
}: {
  parsed: ParsedArgs | null;
  result: string | undefined;
  ok: boolean | undefined;
}) {
  const pattern = str(parsed?.pattern);
  const include = str(parsed?.include) ?? str(parsed?.glob);
  return (
    <>
      <Section label="pattern" mono>
        <span className="tool-prompt">/</span>
        {pattern}
        <span className="tool-prompt">/</span>
        {include && (
          <span style={{ marginLeft: 8, color: "var(--text-3)", fontSize: 11 }}>in {include}</span>
        )}
      </Section>
      {result !== undefined && (
        <Section label={ok === false ? "error" : "matches"}>
          {ok === false ? (
            <CollapsibleOutput text={result} />
          ) : (
            <GrepResults text={result} />
          )}
        </Section>
      )}
    </>
  );
}

function GrepResults({ text }: { text: string }) {
  const [open, setOpen] = useState(false);
  const lines = text.split("\n").filter((l) => l.length > 0);
  if (lines.length === 0) return <span style={{ color: "var(--text-4)" }}>no matches</span>;
  const max = 12;
  const shown = open || lines.length <= max ? lines : lines.slice(0, max);
  return (
    <>
      <div style={{ color: "var(--text-3)", fontSize: 10.5, marginBottom: 6 }}>
        {lines.length} match{lines.length === 1 ? "" : "es"}
      </div>
      <div className="tool-pre">
        {shown.map((line, i) => {
          const m = /^([^:]+):(\d+):(.*)$/.exec(line);
          if (m) {
            return (
              <div key={i} className="tool-match">
                <span className="tool-match-path">{m[1]}</span>
                <span className="tool-match-line">:{m[2]}</span>
                <span className="tool-match-text">{m[3]}</span>
              </div>
            );
          }
          return <div key={i}>{line}</div>;
        })}
      </div>
      {lines.length > max && (
        <button type="button" className="tool-more" onClick={() => setOpen((v) => !v)}>
          {open ? "less" : `+ ${lines.length - max} more`}
        </button>
      )}
    </>
  );
}

/* ─── glob / ls ─── */
function ListBody({
  parsed,
  result,
  ok,
}: {
  parsed: ParsedArgs | null;
  result: string | undefined;
  ok: boolean | undefined;
}) {
  const pattern = str(parsed?.pattern) ?? str(parsed?.path) ?? ".";
  return (
    <>
      <Section label="path" mono>
        {pattern}
      </Section>
      {result !== undefined && (
        <Section label={ok === false ? "error" : "entries"}>
          {ok === false ? (
            <CollapsibleOutput text={result} />
          ) : (
            <ListResults text={result} />
          )}
        </Section>
      )}
    </>
  );
}

function ListResults({ text }: { text: string }) {
  const lines = text.split("\n").filter((l) => l.length > 0);
  if (lines.length === 0) return <span style={{ color: "var(--text-4)" }}>(empty)</span>;
  const [open, setOpen] = useState(false);
  const max = 16;
  const shown = open || lines.length <= max ? lines : lines.slice(0, max);
  return (
    <>
      <div style={{ color: "var(--text-3)", fontSize: 10.5, marginBottom: 6 }}>
        {lines.length} entries
      </div>
      <div className="tool-pre">
        {shown.map((line, i) => (
          <div key={i} className="tool-list-row">
            {line.endsWith("/") ? (
              <FolderOpen size={11} className="tool-list-icon dir" />
            ) : (
              <FileText size={11} className="tool-list-icon" />
            )}
            {line}
          </div>
        ))}
      </div>
      {lines.length > max && (
        <button type="button" className="tool-more" onClick={() => setOpen((v) => !v)}>
          {open ? "less" : `+ ${lines.length - max} more`}
        </button>
      )}
    </>
  );
}

/* ─── todo_write ─── */
function TodoBody({
  parsed,
  result,
  ok,
}: {
  parsed: ParsedArgs | null;
  result: string | undefined;
  ok: boolean | undefined;
}) {
  const items = (parsed?.items ?? parsed?.todos) as unknown;
  const list = Array.isArray(items) ? (items as ParsedArgs[]) : null;
  if (!list || list.length === 0) {
    return result !== undefined ? (
      <Section label={ok === false ? "error" : "result"}>
        <CollapsibleOutput text={result} />
      </Section>
    ) : null;
  }
  return (
    <>
      <Section label={`${list.length} item${list.length === 1 ? "" : "s"}`} mono={false}>
        <div className="todo-list">
          {list.map((it, i) => {
            const status = str(it.status) ?? "pending";
            const text = str(it.content) ?? str(it.text) ?? "—";
            return (
              <div key={i} className={`todo-item st-${status}`}>
                <span className="todo-mark">
                  {status === "completed" ? "✓" : status === "in_progress" ? "•" : "○"}
                </span>
                <span className="todo-text">{text}</span>
              </div>
            );
          })}
        </div>
      </Section>
      {result !== undefined && ok === false && (
        <Section label="error">
          <CollapsibleOutput text={result} />
        </Section>
      )}
    </>
  );
}

/* ─── submit_plan ─── */
function PlanBody({
  parsed,
  result,
  ok,
}: {
  parsed: ParsedArgs | null;
  result: string | undefined;
  ok: boolean | undefined;
}) {
  const steps = parsed?.steps;
  const list = Array.isArray(steps) ? (steps as ParsedArgs[]) : null;
  return (
    <>
      {list && (
        <Section label={`${list.length} step${list.length === 1 ? "" : "s"}`} mono={false}>
          <div className="plan-list">
            {list.map((s, i) => {
              const title = str(s.title) ?? str(s.summary) ?? `Step ${i + 1}`;
              const risk = str(s.risk);
              return (
                <div key={i} className="plan-step">
                  <span className="plan-num">{i + 1}</span>
                  <span className="plan-text">{title}</span>
                  {risk && <span className={`plan-risk r-${risk}`}>{risk}</span>}
                </div>
              );
            })}
          </div>
        </Section>
      )}
      {result !== undefined && (
        <Section label={ok === false ? "error" : "result"}>
          <CollapsibleOutput text={result} maxLines={6} />
        </Section>
      )}
    </>
  );
}

/* ─── ask_choice ─── */
function ChoiceBody({
  parsed,
  result,
  ok,
}: {
  parsed: ParsedArgs | null;
  result: string | undefined;
  ok: boolean | undefined;
}) {
  const q = str(parsed?.question);
  const options = parsed?.options;
  const list = Array.isArray(options) ? (options as unknown[]) : null;
  return (
    <>
      {q && (
        <Section label="question" mono={false}>
          {q}
        </Section>
      )}
      {list && (
        <Section label={`${list.length} option${list.length === 1 ? "" : "s"}`} mono={false}>
          <div className="choice-list">
            {list.map((o, i) => {
              const label = typeof o === "string" ? o : str((o as ParsedArgs).label) ?? String(i + 1);
              return (
                <div key={i} className="choice-opt">
                  <span className="choice-letter">{String.fromCharCode(65 + i)}</span>
                  <span>{label}</span>
                </div>
              );
            })}
          </div>
        </Section>
      )}
      {result !== undefined && (
        <Section label={ok === false ? "error" : "answer"}>
          <CollapsibleOutput text={result} />
        </Section>
      )}
    </>
  );
}

/* ─── memory ─── */
function MemoryBody({
  parsed,
  result,
  ok,
}: {
  parsed: ParsedArgs | null;
  result: string | undefined;
  ok: boolean | undefined;
}) {
  const scope = str(parsed?.scope);
  const key = str(parsed?.key);
  const value = str(parsed?.value);
  const query = str(parsed?.query);
  return (
    <>
      <Section label="memory" mono={false}>
        <div style={{ display: "flex", gap: 10, fontSize: 12 }}>
          {scope && <span><span style={{ color: "var(--text-3)" }}>scope:</span> {scope}</span>}
          {key && <span><span style={{ color: "var(--text-3)" }}>key:</span> {key}</span>}
          {query && <span><span style={{ color: "var(--text-3)" }}>query:</span> {query}</span>}
        </div>
        {value && <div className="tool-pre" style={{ marginTop: 8 }}>{value}</div>}
      </Section>
      {result !== undefined && (
        <Section label={ok === false ? "error" : "result"}>
          <CollapsibleOutput text={result} />
        </Section>
      )}
    </>
  );
}
