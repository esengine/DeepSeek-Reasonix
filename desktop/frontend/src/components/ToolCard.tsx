import { memo, useEffect, useRef, useState, type ReactNode } from "react";
import { ChevronRight, Compass } from "lucide-react";
import { CodeViewer } from "./CodeViewer";
import { DiffView } from "./DiffView";
import { useT } from "../lib/i18n";
import { diffsFor, languageForToolArgs, subjectOf, summarize, summarizeFileDiff } from "../lib/tools";
import { useShellExpand } from "../lib/shellExpand";
import { app } from "../lib/bridge";
import { useCollapseAnimation } from "../lib/useCollapseAnimation";
import { isBatchedReadOnlyTool, isTerminalSubagentPhase, type Item, type SubagentPhase } from "../lib/useController";
import type { Translator } from "../lib/i18n";
import { ReadOnlyBatch } from "./ReadOnlyBatch";
import { Markdown } from "./Markdown";
import { ReasoningSummary } from "./ReasoningSummary";
import { useReasoningDisplayMode } from "../lib/reasoningDisplayPreference";
import { useTranscriptUserResizeIntent } from "./TranscriptLayoutIntentContext";
import { resolveToolCardDefaultOpen } from "../lib/transcriptRowGeometry";
import type { SearchSourcePresentation } from "../lib/searchSourcesPresentation";

type ToolItem = Extract<Item, { kind: "tool" }>;

const SUBAGENT_TOOLS = new Set(["task", "run_skill", "explore", "research", "review", "security_review"]);

function subagentPhaseLabel(t: Translator, phase: SubagentPhase): string {
  switch (phase) {
    case "queued": return t("subagent.phase.queued");
    case "running": return t("subagent.phase.running");
    case "reasoning": return t("subagent.phase.reasoning");
    case "responding": return t("subagent.phase.responding");
    case "tool": return t("subagent.phase.tool");
    case "retrying": return t("subagent.phase.retrying");
    case "completed": return t("subagent.phase.completed");
    case "failed": return t("subagent.phase.failed");
    case "cancelled": return t("subagent.phase.cancelled");
  }
}

function formatElapsedSeconds(ms: number): string {
  return String(Math.max(0, Math.round(ms / 1000)));
}

/** Lines shown by default in a shell output block before the "show all" button. */
const SHELL_PREVIEW_LINES = 10;
const ERROR_SUMMARY_MAX_CHARS = 140;
const ERROR_DETAILS_THRESHOLD = 220;

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

function shellDisplayName(execution?: { shell?: string; shellVersion?: string }): string {
  switch (execution?.shell) {
    case "git-bash":
      return "Git Bash";
    case "powershell":
      return "Windows PowerShell";
    case "pwsh":
      return "PowerShell 7+";
    case "bash":
      return "bash";
    default:
      return execution?.shell || "bash";
  }
}

function shellSettledSummary(
  t: Translator,
  execution: NonNullable<ToolItem["execution"]>,
  durationMs?: number,
): string {
  const parts: string[] = [];
  if (typeof execution.exitCode === "number") {
    parts.push(t("tool.shell.exitCode", { code: execution.exitCode }));
  }
  if (execution.failurePhase) {
    parts.push(execution.failurePhase);
  }
  const ms = execution.durationMs || durationMs;
  if (typeof ms === "number" && Number.isFinite(ms) && ms >= 0) {
    parts.push(formatToolDuration(ms));
  }
  return parts.join(" · ");
}

function shellVerificationLabel(t: Translator, verification?: string): string {
  switch (verification) {
    case "passed":
      return t("tool.shell.verificationPassed");
    case "failed":
      return t("tool.shell.verificationFailed");
    case "not_run":
      return t("tool.shell.verificationNotRun");
    default:
      return "";
  }
}

function shellRiskLabel(t: Translator, execution?: ToolItem["execution"]): string {
  if (!execution) return "";
  const phase = execution.failurePhase || "";
  // Pre-run / not-started phases never touched disk.
  if (phase === "preflight" || phase === "authorization" || phase === "dependency" || phase === "launch") {
    return t("tool.shell.notExecuted");
  }
  // Backend marks failed, timed_out, and cancelled execution as may_be_partial
  // when the process may already have written files. Show the warning for any
  // such risk, not only state=failed.
  if (execution.mutationRisk === "may_be_partial") {
    return t("tool.shell.mayBePartial");
  }
  return "";
}

function firstTailLine(tail?: string): string {
  if (!tail) return "";
  const line = tail.replace(/\r\n/g, "\n").trim().split("\n")[0]?.trim() ?? "";
  if (line.length <= ERROR_SUMMARY_MAX_CHARS) return line;
  return `${line.slice(0, ERROR_SUMMARY_MAX_CHARS - 1)}…`;
}

function formatArgChars(chars: number): string {
  if (chars >= 1000) return `${(chars / 1000).toFixed(1)}k`;
  return String(chars);
}

function normalizeErrorText(text: string): string {
  return text.replace(/\r\n/g, "\n").trim();
}

function withoutErrorPrefix(text: string): string {
  return normalizeErrorText(text).replace(/^error:\s*/i, "");
}

function toolOutputDuplicatesError(output: string | undefined, error: string | undefined): boolean {
  if (!output || !error) return false;
  const normalizedOutput = normalizeErrorText(output);
  const normalizedError = normalizeErrorText(error);
  if (!normalizedOutput || !normalizedError) return false;
  return normalizedOutput === normalizedError || withoutErrorPrefix(normalizedOutput) === withoutErrorPrefix(normalizedError);
}

function summarizeToolError(error: string, receiptMismatchText: string): string {
  const text = withoutErrorPrefix(error);
  if (!text) return "";
  if (/has no matching successful receipt/i.test(text)) {
    return receiptMismatchText;
  }
  const firstLine = text.split("\n")[0]?.trim() ?? "";
  if (firstLine.length <= ERROR_SUMMARY_MAX_CHARS) return firstLine;
  return `${firstLine.slice(0, ERROR_SUMMARY_MAX_CHARS - 1)}…`;
}

function errorNeedsDetails(error: string, summary: string): boolean {
  const normalizedError = withoutErrorPrefix(error);
  if (!normalizedError) return false;
  return normalizedError.includes("\n") ||
    normalizedError.length > ERROR_DETAILS_THRESHOLD ||
    (summary !== "" && normalizedError !== summary);
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
export const ToolCard = memo(function ToolCard({ item, subcalls, tabId, displayName }: { item: ToolItem; subcalls?: ToolItem[]; tabId?: string; displayName?: string }) {
  const t = useT();
  const beginUserResize = useTranscriptUserResizeIntent();
  const nested = subcalls ?? [];
  const hasNested = nested.length > 0;
  const isSubagent = SUBAGENT_TOOLS.has(item.name);
  const profileText =
    isSubagent && item.profile
      ? [item.profile.model, item.profile.effort ? `effort ${item.profile.effort}` : ""].filter(Boolean).join(" · ")
      : "";

  // Sub-agent progress chip: phase + running elapsed + recent activity. The
  // 1s ticker only runs while a progress card is live; terminal cards show
  // the final duration instead.
  const sp = item.subagentProgress;
  const [nowTick, setNowTick] = useState(() => Date.now());
  useEffect(() => {
    if (!sp || isTerminalSubagentPhase(sp.phase)) return;
    const id = window.setInterval(() => setNowTick(Date.now()), 1000);
    return () => window.clearInterval(id);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [sp]);
  const subagentChip = sp
    ? (() => {
        const label = subagentPhaseLabel(t, sp.phase);
        if (isTerminalSubagentPhase(sp.phase)) {
          const ms = sp.durationMs ?? item.durationMs ?? 0;
          return `${label} · ${t("subagent.phase.elapsed", { n: formatElapsedSeconds(ms) })}`;
        }
        return `${label} · ${t("subagent.phase.elapsed", { n: formatElapsedSeconds(nowTick - sp.startedAt) })} · ${t("subagent.activity.ago", { n: formatElapsedSeconds(nowTick - sp.lastActivityAt) })}`;
      })()
    : "";
  const reasoningDisplayMode = useReasoningDisplayMode();
  const hasSubagentPreview = Boolean(sp && ((sp.reasoning && reasoningDisplayMode !== "hidden" && reasoningDisplayMode !== "pending") || sp.text || sp.notice));

  // All tools default to collapsed. Sub-agent tools open while running so the
  // user sees nested calls; they collapse when done. Reasoning (AssistantMessage)
  // also opens while streaming and closes on finish.
  const subagentReasoningRunning = sp?.phase === "reasoning";
  const liveFollow = reasoningDisplayMode === "auto" || reasoningDisplayMode === "expanded";
  const defaultOpen = resolveToolCardDefaultOpen(item, nested.length, reasoningDisplayMode);
  const [userOpen, setUserOpen] = useState<boolean | null>(null);
  const open = userOpen ?? defaultOpen;
  const openRef = useRef(open);
  openRef.current = open;
  const [showAll, setShowAll] = useState(false);
  const [showErrorDetails, setShowErrorDetails] = useState(false);
  // The sub-agent reasoning preview opens as a one-line summary; the full
  // Markdown only mounts after the user expands the reasoning section.
  const [subagentReasoningOpen, setSubagentReasoningOpen] = useState(
    () => reasoningDisplayMode === "expanded" || (reasoningDisplayMode === "auto" && subagentReasoningRunning),
  );
  const subagentReasoningUserOverridden = useRef(false);
  const previousSubagentReasoningRunning = useRef(subagentReasoningRunning);
  const previousReasoningDisplayMode = useRef(reasoningDisplayMode);
  useEffect(() => {
    const modeChanged = previousReasoningDisplayMode.current !== reasoningDisplayMode;
    const wasRunning = previousSubagentReasoningRunning.current;
    previousReasoningDisplayMode.current = reasoningDisplayMode;
    previousSubagentReasoningRunning.current = subagentReasoningRunning;
    if (modeChanged) {
      subagentReasoningUserOverridden.current = false;
      setSubagentReasoningOpen(reasoningDisplayMode === "expanded" || (reasoningDisplayMode === "auto" && subagentReasoningRunning));
      return;
    }
    if (subagentReasoningRunning && !wasRunning) {
      subagentReasoningUserOverridden.current = false;
      if (liveFollow) setSubagentReasoningOpen(true);
      return;
    }
    if (reasoningDisplayMode !== "auto") return;
    if (!subagentReasoningRunning && wasRunning && !subagentReasoningUserOverridden.current) {
      setSubagentReasoningOpen(false);
    }
  }, [reasoningDisplayMode, subagentReasoningRunning]);
  // Lazy-load full tool data from the backend when the card is expanded and
  // the in-memory copy was archived for memory efficiency.
  const [fullData, setFullData] = useState<{ args: string; output?: string; execution?: ToolItem["execution"] } | null>(null);
  const archivedWithoutFullData = Boolean(item.dataArchived && !fullData);
  const effectiveArgs = archivedWithoutFullData ? "" : fullData?.args ?? item.args;
  const effectiveOutput = fullData?.output ?? item.output;
  const execution = fullData?.execution ?? item.execution;
  const isWebSearch = item.name === "web_search";
  const [searchPresentation, setSearchPresentation] = useState<SearchSourcePresentation | null>(null);
  useEffect(() => {
    let cancelled = false;
    if (!isWebSearch) { setSearchPresentation(null); return () => { cancelled = true; }; }
    void import("../lib/searchSourcesPresentation").then(({ normalizeSearchSources }) => {
      if (!cancelled) setSearchPresentation(normalizeSearchSources(item.searchSources));
    });
    return () => { cancelled = true; };
  }, [isWebSearch, item.searchSources]);
  const searchVisibleCount = searchPresentation?.visible.length ?? item.searchSources?.length ?? 0;
  const searchHiddenCount = searchPresentation?.hiddenCount ?? 0;
  const searchResultLabel = isWebSearch && searchVisibleCount === 0 && searchHiddenCount > 0
    ? t("sources.noValid")
    : t("tool.searchResults", { n: searchVisibleCount });
  const isShellCard = Boolean(item.isShell || item.name === "bash" || execution);
  const displayOutput = isWebSearch || toolOutputDuplicatesError(effectiveOutput, item.error) ? undefined : effectiveOutput;
  const previewDiff = item.fileDiff?.diff ? item.fileDiff : undefined;
  const diffs = previewDiff || archivedWithoutFullData ? [] : diffsFor(item.name, effectiveArgs);
  const subject = fullData ? subjectOf(item.name, effectiveArgs) : item.subject || subjectOf(item.name, effectiveArgs);
  const shellName = isShellCard ? shellDisplayName(execution) : (displayName ?? item.name);
  const shellSummary = execution && item.status !== "running" ? shellSettledSummary(t, execution, item.durationMs) : "";
  const verificationLabel = shellVerificationLabel(t, execution?.verification);
  const riskLabel = shellRiskLabel(t, execution);
  const tailSummary = firstTailLine(execution?.outputTail);
  // Reset cached fullData when the item identity changes (e.g. after rewind).
  useEffect(() => {
    return () => setFullData(null);
  }, [item]);

  // edit diffs are the point of the card, so they're shown inline; everything
  // else folds its args/output away by default.  Open while running so the
  // user sees progress; closed by default once settled.
  const hasArchivedOnDemandBody = Boolean(item.dataArchived && tabId);
  const hasArgsOrOutput = !previewDiff && diffs.length === 0 && (isWebSearch
    ? Boolean(effectiveArgs || searchVisibleCount || searchHiddenCount)
    : Boolean(effectiveArgs || displayOutput || hasArchivedOnDemandBody));

  // Shell output: split into preview + "show all" toggle.
  const shellOutput = isShellCard && displayOutput ? displayOutput : null;
  const shellPreview = shellOutput ? splitPreview(shellOutput, SHELL_PREVIEW_LINES) : null;
  const hasStderrDetails = Boolean(execution?.outputTail && execution.outputTail.trim());
  const hasBody = Boolean(previewDiff || diffs.length || hasNested || shellPreview || (!shellPreview && hasArgsOrOutput) || item.error || hasSubagentPreview || hasStderrDetails || riskLabel || verificationLabel);
  const errorText = item.error ? normalizeErrorText(item.error) : "";
  const errorSummary = errorText ? summarizeToolError(errorText, t("tool.errorReceiptMismatch")) : "";
  const hasErrorDetails = errorText ? errorNeedsDetails(errorText, errorSummary) : false;
  useEffect(() => {
    if (!open || !item.dataArchived || fullData || !tabId) return;
    let cancelled = false;
    void app.ToolResultForTab(tabId, item.id).then((d) => {
      if (!cancelled && d) setFullData(d);
    }).catch(() => {});
    return () => { cancelled = true; };
  }, [open, item.id, item.dataArchived, fullData, tabId]);

  // Register this shell card's toggle with the global ShellExpand context so
  // Ctrl/Cmd+B can expand/collapse the most recent shell output. openRef keeps the
  // registered closure flipping the current state, not a stale one.
  const shellExpand = useShellExpand();
  useEffect(() => {
    if (!isShellCard || !shellExpand) return;
    return shellExpand.register(item.id, () => setUserOpen(!openRef.current));
  }, [isShellCard, item.id, shellExpand]);

  // Read-only "research" calls (read/grep/ls/glob/web_fetch) are hidden after
  // completion so they don't clutter the transcript. During execution they still
  // render so the user sees progress.
  const quiet =
    item.readOnly && item.name !== "web_search" && !hasNested && item.status !== "error" && item.status !== "stopped";

  const duration = item.status === "running" ? "" : (shellSummary || formatToolDuration(item.durationMs));
  // While the model is still streaming this call's arguments (partial
  // dispatch), show the received volume as the live subject so a long
  // write_file body reads as progress instead of a silent stall.
  const streamingArgs = item.status === "running" && !item.args && (item.argChars ?? 0) > 0
    ? t("tool.receivingArgs", { chars: formatArgChars(item.argChars ?? 0) })
    : "";
  const summary = item.status === "running"
    ? streamingArgs
    : (isWebSearch
      ? (item.error ? (tailSummary || errorSummary) : searchResultLabel)
      : (verificationLabel || item.summary || summarizeFileDiff(item.fileDiff) || (item.error ? (tailSummary || errorSummary) : archivedWithoutFullData ? "" : summarize(item.name, effectiveArgs, displayOutput, item.error))));
  const a11yLabel = isShellCard
    ? `${shellName} ${item.status}${shellSummary || summary ? ` ${shellSummary || summary}` : ""}`
    : undefined;

  // Native collapse/expand for the tool body.
  const toolBodyRef = useRef<HTMLDivElement>(null);
  useCollapseAnimation(toolBodyRef, open);

  return (
    <div
      className={`tool${quiet ? " tool--quiet" : ""}${isSubagent ? " tool--subagent" : ""}${open && hasBody ? " tool--open" : ""}`}
      data-entrance={item.id}
      data-shell={isShellCard ? execution?.shell || "bash" : undefined}
      data-transcript-layout-variant={open && hasBody ? "tool-expanded" : "tool-collapsed"}
    >
      <button
        type="button"
        className="tool__head"
        data-running={item.status === "running" ? "" : undefined}
        onClick={() => { if (hasBody) { beginUserResize(); setUserOpen(!open); } }}
        aria-expanded={hasBody ? open : undefined}
        aria-label={a11yLabel}
      >
        <span className="tool__label-group">
          {hasNested && (
            <span className="tool__nested-count" aria-label={`${nested.length} nested tool calls`}>
              <Compass className="tool__nested-icon" size={14} strokeWidth={2} aria-hidden="true" />
              <span>{nested.length}</span>
            </span>
          )}
          {item.status === "error" && <span className="tool__status-icon tool__status-icon--err">✗</span>}
          {item.status === "done" && <span className="tool__status-icon tool__status-icon--ok">✓</span>}
          {item.status === "stopped" && <span className="tool__status-icon tool__status-icon--stopped">—</span>}
          <span className="tool__name">{isShellCard ? shellName : (displayName ?? item.name)}</span>
          {subject && <span className="tool__subject">{subject}</span>}
        </span>
        {profileText && <span className="tool__profile">{profileText}</span>}
        {subagentChip && (
          <span className={`tool__subagent-chip tool__subagent-chip--${sp?.phase}`} data-phase={sp?.phase}>
            <span className="tool__subagent-dot" aria-hidden="true" />
            {subagentChip}
          </span>
        )}
        {summary && <span className="tool__summary">{summary}</span>}
        {duration && <span className="tool__duration">{duration}</span>}
        {hasBody && (
          <span className={`tool__chevron${open ? " tool__chevron--open" : ""}`}>
            <ChevronRight size={12} />
          </span>
        )}
        {item.status !== "running" && (
          <span
            className={`tool__dot${item.status === "done" ? " tool__dot--ok" : ""}${item.status === "error" ? " tool__dot--err" : ""}${item.status === "stopped" ? " tool__dot--stopped" : ""}`}
            aria-hidden="true"
          />
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

        {open && hasSubagentPreview && sp && (
          <div className="tool__subagent-preview">
            {sp.reasoning && reasoningDisplayMode !== "hidden" && reasoningDisplayMode !== "pending" && (
              <div className="tool__subagent-preview-section">
                <button
                  type="button"
                  className="tool__subagent-preview-label tool__subagent-preview-label--toggle"
                  onClick={() => {
                    beginUserResize();
                    subagentReasoningUserOverridden.current = true;
                    const next = !subagentReasoningOpen;
                    if (next) setUserOpen(true);
                    setSubagentReasoningOpen(next);
                  }}
                  aria-expanded={subagentReasoningOpen}
                >
                  {t("subagent.preview.reasoning")}
                </button>
                {subagentReasoningOpen ? (
                  <div className="tool__subagent-preview-text tool__subagent-preview-text--markdown">
                    <Markdown text={sp.reasoning} streaming={sp.phase === "reasoning"} />
                  </div>
                ) : (
                  <ReasoningSummary
                    text={sp.reasoning}
                    streaming={sp.phase === "reasoning"}
                    onOpen={() => {
                      beginUserResize();
                      subagentReasoningUserOverridden.current = true;
                      setUserOpen(true);
                      setSubagentReasoningOpen(true);
                    }}
                  />
                )}
              </div>
            )}
            {sp.text && (
              <div className="tool__subagent-preview-section">
                <div className="tool__subagent-preview-label">{t("subagent.preview.text")}</div>
                <pre className="tool__subagent-preview-text">{sp.text}</pre>
              </div>
            )}
            {sp.notice && (
              <div className="tool__subagent-preview-section">
                <div className="tool__subagent-preview-label">{t("subagent.preview.notice")}</div>
                <pre className="tool__subagent-preview-text">{sp.notice}</pre>
              </div>
            )}
            {sp.truncated && <div className="tool__note">{t("subagent.preview.truncated")}</div>}
          </div>
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
                if (isBatchedReadOnlyTool(c.name, c.readOnly)) {
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

        {isShellCard && (riskLabel || verificationLabel) && (
          <div className="tool__note" role="status">
            {[riskLabel, verificationLabel].filter(Boolean).join(" · ")}
          </div>
        )}

        {shellPreview && (
          <>
            <CodeViewer value={showAll ? shellOutput! : shellPreview.preview} maxHeight={showAll ? 480 : 260} />
            {shellPreview.hasMore && !showAll && (
              <button className="tool__showall" onClick={() => { beginUserResize(); setShowAll(true); }}>
                {t("tool.showAllLines", { n: shellPreview.total })}
              </button>
            )}
            {item.truncated && <div className="tool__note">{t("tool.truncated")}</div>}
          </>
        )}

        {hasStderrDetails && (
          <details className="tool__error-details">
            <summary>{tailSummary || t("tool.showErrorDetails")}</summary>
            <CodeViewer value={execution!.outputTail!} maxHeight={240} />
          </details>
        )}

        {isWebSearch && hasArgsOrOutput && (
          <div className="tool__search-summary">
            {subject && <div className="tool__search-query">{t("tool.searchQuery", { query: subject })}</div>}
            <div className="tool__search-count">
              {searchResultLabel}
              {searchHiddenCount > 0 && ` · ${t("sources.hidden", { n: searchHiddenCount })}`}
            </div>
          </div>
        )}

        {!isWebSearch && !shellPreview && hasArgsOrOutput && (
          <>
            {effectiveArgs && <CodeViewer value={pretty(effectiveArgs)} language="json" maxHeight={180} />}
            {displayOutput && (
              <>
                <CodeViewer value={displayOutput} maxHeight={280} />
                {item.truncated && <div className="tool__note">{t("tool.truncated")}</div>}
              </>
            )}
          </>
        )}

        {errorText && (
          <div className={`tool__err${hasErrorDetails ? " tool__err--compact" : ""}`}>
            {hasErrorDetails ? (
              <>
                <div className="tool__err-summary">{errorSummary || t("tool.error")}</div>
                <button
                  type="button"
                  className="tool__err-toggle"
                  onClick={() => { beginUserResize(); setShowErrorDetails((value) => !value); }}
                  aria-expanded={showErrorDetails}
                >
                  <ChevronRight className={`tool__err-toggle-icon${showErrorDetails ? " tool__err-toggle-icon--open" : ""}`} size={12} aria-hidden="true" />
                  <span>{showErrorDetails ? t("tool.hideErrorDetails") : t("tool.showErrorDetails")}</span>
                </button>
                {showErrorDetails && <div className="tool__err-details">{errorText}</div>}
              </>
            ) : (
              errorText
            )}
          </div>
        )}
      </div>
    </div>
  );
});
