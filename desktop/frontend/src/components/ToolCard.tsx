import { memo, useEffect, useState } from "react";
import { CodeViewer } from "./CodeViewer";
import { DiffView } from "./DiffView";
import { ProcessCard, ProcessStatusIcon, ProcessToolIcon, type ProcessState, type ProcessTone } from "./ProcessCard";
import { useT } from "../lib/i18n";
import { diffsFor, subjectOf, summarize } from "../lib/tools";
import { useShellExpand } from "../lib/shellExpand";
import type { Item } from "../lib/useController";

type ToolItem = Extract<Item, { kind: "tool" }>;

const SUBAGENT_TOOLS = new Set(["task", "run_skill", "explore", "research", "review", "security_review"]);

/** Lines shown by default in a shell output block before the "show all" button. */
const SHELL_PREVIEW_LINES = 10;

function pretty(json: string): string {
  try {
    return JSON.stringify(JSON.parse(json), null, 2);
  } catch {
    return json;
  }
}

function processState(status: ToolItem["status"]): ProcessState {
  if (status === "running") return "running";
  if (status === "error") return "failed";
  if (status === "stopped") return "stopped";
  return "done";
}

function processTone(status: ToolItem["status"]): ProcessTone {
  if (status === "error") return "danger";
  if (status === "stopped") return "warning";
  if (status === "done") return "success";
  return "default";
}

function formatDuration(ms?: number): string {
  if (typeof ms !== "number" || !Number.isFinite(ms) || ms < 0) return "";
  return `${Math.round(ms)} ms`;
}

/** Returns the first n lines of text and the total line count. */
function splitPreview(text: string, n: number): { preview: string; total: number; hasMore: boolean } {
  const lines = text.split("\n");
  const total = lines.length;
  if (total <= n) return { preview: text, total, hasMore: false };
  return { preview: lines.slice(0, n).join("\n"), total, hasMore: true };
}

// ToolCard renders one tool call. `subcalls` are sub-agent calls nested under a
// `task` card (their ParentID points at this call); they render inline, live, so
// the sub-agent's work is visible as it happens.
export const ToolCard = memo(function ToolCard({ item, subcalls }: { item: ToolItem; subcalls?: ToolItem[] }) {
  const t = useT();
  const diffs = diffsFor(item.name, item.args);
  const subject = subjectOf(item.name, item.args);
  const nested = subcalls ?? [];
  const hasNested = nested.length > 0;
  const profileText =
    SUBAGENT_TOOLS.has(item.name) && item.profile
      ? [item.profile.model, item.profile.effort ? `effort ${item.profile.effort}` : ""].filter(Boolean).join(" · ")
      : "";

  // A task's summary is its step count; everything else derives from the result.
  const summary =
    item.status === "running"
      ? ""
      : hasNested
        ? t(nested.length === 1 ? "tool.stepOne" : "tool.stepOther", { n: nested.length })
        : summarize(item.name, item.args, item.output, item.error);

  // edit diffs are the point of the card, so they're shown inline; everything
  // else folds its args/output away by default. Nested children always show.
  // Shell commands default to open so the output is immediately visible.
  const hasArgsOrOutput = diffs.length === 0 && (!!item.args || !!item.output);
  const [open, setOpen] = useState(item.isShell && hasArgsOrOutput);
  const [showAll, setShowAll] = useState(false);

  // Register this shell card's toggle with the global ShellExpand context so
  // Ctrl/Cmd+B can expand/collapse the most recent shell output.
  const shellExpand = useShellExpand();
  useEffect(() => {
    if (!item.isShell || !shellExpand) return;
    return shellExpand.register(item.id, () => setOpen((v) => !v));
  }, [item.isShell, item.id, shellExpand]);

  // Read-only "research" calls (read/grep/ls/glob/web_fetch) are quieted to a
  // slim, borderless, dim row so a long run of them doesn't bury the few calls
  // that matter — writers, bash, sub-agents, and anything that failed keep the
  // full card. Uses the readOnly flag, not a tool-name list.
  const quiet =
    item.readOnly && !hasNested && item.status !== "error" && item.status !== "stopped";

  // Shell output: split into preview + "show all" toggle.
  const shellOutput = item.isShell && item.output ? item.output : null;
  const shellPreview = shellOutput ? splitPreview(shellOutput, SHELL_PREVIEW_LINES) : null;
  const hasProcessBody = Boolean(summary || diffs.length || hasNested || shellPreview || (!shellPreview && hasArgsOrOutput) || item.error);
  const duration = item.status === "running" ? "" : formatDuration(item.durationMs);

  return (
    <ProcessCard
      tone={processTone(item.status)}
      icon={<ProcessToolIcon size={12} />}
      kind="tool"
      name={
        <>
          <span className="tool__name">{item.name}</span>
          {subject && <span className="tool__subject">{subject}</span>}
          {profileText && <span className="tool__profile">{profileText}</span>}
        </>
      }
      meta={
        <>
          <ProcessStatusIcon state={processState(item.status)} label={item.status} />
          {duration && <span className="tool__duration">{duration}</span>}
        </>
      }
      open={hasProcessBody ? open : undefined}
      onOpenChange={hasProcessBody ? setOpen : undefined}
      defaultOpen={item.isShell && hasArgsOrOutput}
      className={`tool tool--${item.status}${quiet ? " tool--quiet" : ""}`}
    >

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

      {/* Shell output: always visible (auto-open), with preview/show-all toggle */}
      {shellPreview && (
        <div className="tool__body">
          <CodeViewer value={showAll ? shellOutput! : shellPreview.preview} maxHeight={showAll ? 480 : 260} />
          {shellPreview.hasMore && !showAll && (
            <button className="tool__showall" onClick={() => setShowAll(true)}>
              {t("tool.showAllLines", { n: shellPreview.total })}
            </button>
          )}
          {item.truncated && <div className="tool__note">{t("tool.truncated")}</div>}
        </div>
      )}

      {/* Non-shell body: args + output, gated by open */}
      {!shellPreview && hasArgsOrOutput && (
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
    </ProcessCard>
  );
});
