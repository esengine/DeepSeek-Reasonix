import { memo, useEffect, useRef, useState } from "react";
import { ChevronRight } from "lucide-react";
import { CodeViewer } from "./CodeViewer";
import { DiffView } from "./DiffView";
import { useT } from "../lib/i18n";
import { diffsFor, subjectOf } from "../lib/tools";
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

function formatToolDuration(ms?: number): string {
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
  const isSubagent = SUBAGENT_TOOLS.has(item.name);
  const profileText =
    isSubagent && item.profile
      ? [item.profile.model, item.profile.effort ? `effort ${item.profile.effort}` : ""].filter(Boolean).join(" · ")
      : "";

  // edit diffs are the point of the card, so they're shown inline; everything
  // else folds its args/output away by default.  Open while running so the
  // user sees progress; closed by default once settled.
  const hasArgsOrOutput = diffs.length === 0 && (!!item.args || !!item.output);

  // Shell output: split into preview + "show all" toggle.
  const shellOutput = item.isShell && item.output ? item.output : null;
  const shellPreview = shellOutput ? splitPreview(shellOutput, SHELL_PREVIEW_LINES) : null;
  const hasBody = Boolean(diffs.length || hasNested || shellPreview || (!shellPreview && hasArgsOrOutput) || item.error);
  // Writers keep their output/diff visible by default (don't make the user expand
  // to see what a command produced); read-only research folds away. Sub-agents open
  // while running so nested calls are visible. A click (or Ctrl/Cmd+B) overrides.
  const defaultOpen = hasNested
    ? item.status === "running"
    : Boolean(item.error) || (!item.readOnly && (diffs.length > 0 || !!item.output));
  const [userOpen, setUserOpen] = useState<boolean | null>(null);
  const open = userOpen ?? defaultOpen;
  const openRef = useRef(open);
  openRef.current = open;
  const [showAll, setShowAll] = useState(false);

  // Register this shell card's toggle with the global ShellExpand context so
  // Ctrl/Cmd+B can expand/collapse the most recent shell output. openRef keeps the
  // registered closure flipping the current state, not a stale one.
  const shellExpand = useShellExpand();
  useEffect(() => {
    if (!item.isShell || !shellExpand) return;
    return shellExpand.register(item.id, () => setUserOpen(!openRef.current));
  }, [item.isShell, item.id, shellExpand]);

  // Read-only "research" calls (read/grep/ls/glob/web_fetch) are hidden after
  // completion so they don't clutter the transcript. During execution they still
  // render so the user sees progress.
  const quiet =
    item.readOnly && !hasNested && item.status !== "error" && item.status !== "stopped";

  const duration = item.status === "running" ? "" : formatToolDuration(item.durationMs);

  return (
    <div className={`tool${quiet ? " tool--quiet" : ""}${isSubagent ? " tool--subagent" : ""}`}>
      <button
        type="button"
        className="tool__head"
        data-running={item.status === "running" ? "" : undefined}
        onClick={() => hasBody && setUserOpen(!open)}
        aria-expanded={hasBody ? open : undefined}
      >
        <span className="tool__label-group">
          <span className="tool__name">{item.name}</span>
          {subject && <span className="tool__subject">{subject}</span>}
          {hasNested && <span className="tool__nested-count">⊞{nested.length}</span>}
        </span>
        {profileText && <span className="tool__profile">{profileText}</span>}
        {duration && <span className="tool__duration">{duration}</span>}
        {hasBody && (
          <span className={`tool__chevron${open ? " tool__chevron--open" : ""}`}>
            <ChevronRight size={12} />
          </span>
        )}
      </button>

      {open && (
        <div className="tool__body">

        {diffs.map((d, i) => (
          <div key={i}>
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

        {shellPreview && (
          <>
            <CodeViewer value={showAll ? shellOutput! : shellPreview.preview} maxHeight={showAll ? 480 : 260} />
            {shellPreview.hasMore && !showAll && (
              <button className="tool__showall" onClick={() => setShowAll(true)}>
                {t("tool.showAllLines", { n: shellPreview.total })}
              </button>
            )}
            {item.truncated && <div className="tool__note">{t("tool.truncated")}</div>}
          </>
        )}

        {!shellPreview && hasArgsOrOutput && (
          <>
            {item.args && <CodeViewer value={pretty(item.args)} language="json" maxHeight={180} />}
            {item.output && (
              <>
                <CodeViewer value={item.output} maxHeight={280} />
                {item.truncated && <div className="tool__note">{t("tool.truncated")}</div>}
              </>
            )}
          </>
        )}

        {item.error && <div className="tool__err">{item.error}</div>}
        </div>
      )}
    </div>
  );
});
