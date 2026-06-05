import { memo, useState } from "react";
import {
  Ban,
  Check,
  ChevronRight,
  FilePen,
  FileText,
  FolderOpen,
  Globe,
  Loader2,
  ListTree,
  Search,
  SquareTerminal,
  Wrench,
  X,
  type LucideIcon,
} from "lucide-react";
import { CodeViewer } from "./CodeViewer";
import { DiffView } from "./DiffView";
import { useT } from "../lib/i18n";
import { diffsFor, subjectOf, summarize } from "../lib/tools";
import type { Item } from "../lib/useController";

type ToolItem = Extract<Item, { kind: "tool" }>;

const ICONS: Record<string, LucideIcon> = {
  edit_file: FilePen,
  multi_edit: FilePen,
  write_file: FilePen,
  read_file: FileText,
  bash: SquareTerminal,
  ls: FolderOpen,
  glob: Search,
  grep: Search,
  web_fetch: Globe,
  task: ListTree,
};

function pretty(json: string): string {
  try {
    return JSON.stringify(JSON.parse(json), null, 2);
  } catch {
    return json;
  }
}

function StatusGlyph({ status }: { status: ToolItem["status"] }) {
  if (status === "running") return <Loader2 className="ico spin" size={13} />;
  if (status === "error") return <X className="ico ico--err" size={13} />;
  if (status === "stopped") return <Ban className="ico ico--stopped" size={13} />;
  return <Check className="ico ico--ok" size={13} />;
}

// ToolCard renders one tool call. `subcalls` are sub-agent calls nested under a
// `task` card (their ParentID points at this call); they render inline, live, so
// the sub-agent's work is visible as it happens.
export const ToolCard = memo(function ToolCard({ item, subcalls }: { item: ToolItem; subcalls?: ToolItem[] }) {
  const t = useT();
  const diffs = diffsFor(item.name, item.args);
  const subject = subjectOf(item.name, item.args);
  const Icon = ICONS[item.name] ?? Wrench;
  const nested = subcalls ?? [];
  const hasNested = nested.length > 0;

  // A task's summary is its step count; everything else derives from the result.
  const summary =
    item.status === "running"
      ? ""
      : hasNested
        ? t(nested.length === 1 ? "tool.stepOne" : "tool.stepOther", { n: nested.length })
        : summarize(item.name, item.args, item.output, item.error);

  // edit diffs are the point of the card, so they're shown inline; everything
  // else folds its args/output away by default. Nested children always show.
  const hasBody = diffs.length === 0 && (!!item.args || !!item.output);
  const [open, setOpen] = useState(false);
  const expandable = hasBody;

  // Read-only "research" calls (read/grep/ls/glob/web_fetch) are quieted to a
  // slim, borderless, dim row so a long run of them doesn't bury the few calls
  // that matter — writers, bash, sub-agents, and anything that failed keep the
  // full card. Uses the readOnly flag, not a tool-name list.
  const quiet =
    item.readOnly && !hasNested && item.status !== "error" && item.status !== "stopped";

  return (
    <div className={`tool tool--${item.status} ${quiet ? "tool--quiet" : ""}`}>
      <div
        className={`tool__row ${expandable ? "tool__row--clickable" : ""}`}
        onClick={expandable ? () => setOpen((v) => !v) : undefined}
      >
        {expandable ? (
          <ChevronRight className={`tool__chevron ${open ? "tool__chevron--open" : ""}`} size={13} />
        ) : (
          <span className="tool__chevron tool__chevron--placeholder" />
        )}
        <Icon className="tool__icon" size={14} />
        <span className="tool__name">{item.name}</span>
        {subject && <span className="tool__subject">{subject}</span>}
        <span className="tool__meta">
          <StatusGlyph status={item.status} />
        </span>
      </div>

      {summary && <div className="tool__summary">{summary}</div>}

      {diffs.map((d, i) => (
        <div className="tool__body" key={i}>
          {d.label && <div className="tool__difflabel">{d.label}</div>}
          <DiffView original={d.original} modified={d.modified} language={d.lang} maxHeight={260} />
        </div>
      ))}

      {hasNested && (
        <div className="tool__nested">
          {nested.map((c) => (
            <ToolCard key={c.id} item={c} />
          ))}
        </div>
      )}

      {hasBody && open && (
        <div className="tool__body">
          {item.args && <CodeViewer value={pretty(item.args)} language="json" maxHeight={180} />}
          {item.output && (
            <>
              <CodeViewer value={item.output} maxHeight={280} />
              {item.truncated && <div className="tool__note">{t("tool.truncated")}</div>}
            </>
          )}
        </div>
      )}

      {item.error && <div className="tool__err">{item.error}</div>}
    </div>
  );
});
