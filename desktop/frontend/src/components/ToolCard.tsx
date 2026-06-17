import { memo, useEffect, useRef, useState, type ReactNode } from "react";
import { ChevronRight } from "lucide-react";
import { CodeViewer } from "./CodeViewer";
import { DiffView } from "./DiffView";
import { useT } from "../lib/i18n";
import { diffsFor, languageForToolArgs, subjectOf, summarize, summarizeFileDiff } from "../lib/tools";
import { useShellExpand } from "../lib/shellExpand";
import { useGSAPCollapse } from "../lib/useGSAPCollapse";
import type { Item } from "../lib/useController";
import { isReadOnlyTool } from "../lib/useController";
import { ReadOnlyBatch } from "./ReadOnlyBatch";

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
export const ToolCard = memo(function ToolCard({ item, subcalls, tabId }: { item: ToolItem; subcalls?: ToolItem[]; tabId?: string }) {
  const t = useT();
  const nested = subcalls ?? [];
  const hasNested = nested.length > 0;
  const isSubagent = SUBAGENT_TOOLS.has(item.name);
  const profileText =
    isSubagent && item.profile
      ? [item.profile.model, item.profile.effort ? `effort ${item.profile.effort}` : ""].filter(Boolean).join(" · ")
      : "";

  // All tools default to collapsed. Sub-agent tools open while running so the
  // user sees nested calls; they collapse when done. Reasoning (AssistantMessage)
  // also opens while streaming and closes on finish.
  const defaultOpen = hasNested ? item.status === "running" : false;
  const [userOpen, setUserOpen] = useState<boolean | null>(null);
  const open = userOpen ?? defaultOpen;
  const openRef = useRef(open);
  openRef.current = open;
  const [showAll, setShowAll] = useState(false);
  // Lazy-load full tool data from the backend when the card is expanded and
  // the in-memory copy was archived for memory efficiency.
  const [fullData, setFullData] = useState<{ args: string; output?: string } | null>(null);
  const archivedWithoutFullData = Boolean(item.dataArchived && !fullData);
  const effectiveArgs = archivedWithoutFullData ? "" : fullData?.args ?? item.args;
  const effectiveOutput = fullData?.output ?? item.output;
  const previewDiff = item.fileDiff?.diff ? item.fileDiff : undefined;
  const diffs = previewDiff || archivedWithoutFullData ? [] : diffsFor(item.name, effectiveArgs);
  const subject = fullData ? subjectOf(item.name, effectiveArgs) : item.subject || subjectOf(item.name, effectiveArgs);
  // Reset cached fullData when the item identity changes (e.g. after rewind).
  useEffect(() => {
    return () => setFullData(null);
  }, [item]);

  // edit diffs are the point of the card, so they're shown inline; everything
  // else folds its args/output away by default.  Open while running so the
  // user sees progress; closed by default once settled.
  const hasArchivedOnDemandBody = Boolean(item.dataArchived && tabId);
  const hasArgsOrOutput = !previewDiff && diffs.length === 0 && (!!effectiveArgs || !!effectiveOutput || hasArchivedOnDemandBody);

  // Shell output: split into preview + "show all" toggle.
  const shellOutput = item.isShell && effectiveOutput ? effectiveOutput : null;
  const shellPreview = shellOutput ? splitPreview(shellOutput, SHELL_PREVIEW_LINES) : null;
  const hasBody = Boolean(previewDiff || diffs.length || hasNested || shellPreview || (!shellPreview && hasArgsOrOutput) || item.error);
  useEffect(() => {
    if (!open || !item.dataArchived || fullData || !tabId) return;
    let cancelled = false;
    import("../lib/bridge").then(({ app }) =>
      app.ToolResultForTab(tabId, item.id).then((d) => {
        if (!cancelled && d) setFullData(d);
      }).catch(() => {}),
    );
    return () => { cancelled = true; };
  }, [open, item.id, item.dataArchived, fullData, tabId]);

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
  const summary = item.status === "running" ? "" : item.summary || summarizeFileDiff(item.fileDiff) || (archivedWithoutFullData ? "" : summarize(item.name, effectiveArgs, effectiveOutput, item.error));

  // GSAP-driven collapse/expand for tool body
  const toolBodyRef = useRef<HTMLDivElement>(null);
  useGSAPCollapse(toolBodyRef, open);

  return (
    <div className={`tool${quiet ? " tool--quiet" : ""}${isSubagent ? " tool--subagent" : ""}${open && hasBody ? " tool--open" : ""}`} data-entrance={item.id}>
      <button
        type="button"
        className="tool__head"
        data-running={item.status === "running" ? "" : undefined}
        onClick={() => hasBody && setUserOpen(!open)}
        aria-expanded={hasBody ? open : undefined}
      >
        <span className="tool__label-group">
          {hasNested && <span className="tool__nested-count">⊞{nested.length}</span>}
          {item.status === "error" && <span className="tool__status-icon tool__status-icon--err">✗</span>}
          {item.status === "done" && <span className="tool__status-icon tool__status-icon--ok">✓</span>}
          {item.status === "stopped" && <span className="tool__status-icon tool__status-icon--stopped">—</span>}
          <span className="tool__name">{item.name}</span>
          {subject && <span className="tool__subject">{subject}</span>}
        </span>
        {profileText && <span className="tool__profile">{profileText}</span>}
        {summary && <span className="tool__summary">{summary}</span>}
        {duration && <span className="tool__duration">{duration}</span>}
        {hasBody && (
          <span className={`tool__chevron${open ? " tool__chevron--open" : ""}`}>
            <ChevronRight size={12} />
          </span>
        )}
      </button>

      <div ref={toolBodyRef} className="tool__body">

        {previewDiff ? (
          <DiffView diff={previewDiff.diff} language={languageForToolArgs(fullData?.args ?? item.args)} maxHeight={260} />
        ) : (
          diffs.map((d, i) => (
            <div key={i}>
              {d.label && <div className="tool__difflabel">{d.label}</div>}
              <DiffView original={d.original} modified={d.modified} language={d.lang} maxHeight={260} />
            </div>
          ))
        )}

        {hasNested && (
          <div className="tool__nested">
            {(() => {
              const out: ReactNode[] = [];
              const roBatch: typeof nested = [];
              const flush = () => {
                if (roBatch.length === 0) return;
                out.push(<ReadOnlyBatch key={`rob-${roBatch[0].id}`} items={[...roBatch]} subcalls={new Map()} tabId={tabId} />);
                roBatch.length = 0;
              };
              for (const c of nested) {
                if (isReadOnlyTool(c.name) && c.name !== "todo_write") {
                  roBatch.push(c);
                  continue;
                }
                flush();
                out.push(<ToolCard key={c.id} item={c} tabId={tabId} />);
              }
              flush();
              return out;
            })()}
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
            {effectiveArgs && <CodeViewer value={pretty(effectiveArgs)} language="json" maxHeight={180} />}
            {effectiveOutput && (
              <>
                <CodeViewer value={effectiveOutput} maxHeight={280} />
                {item.truncated && <div className="tool__note">{t("tool.truncated")}</div>}
              </>
            )}
          </>
        )}

        {item.error && <div className="tool__err">{item.error}</div>}
      </div>
    </div>
  );
});
