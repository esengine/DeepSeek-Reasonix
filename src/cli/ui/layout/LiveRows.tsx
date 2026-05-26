import { Box, Text } from "ink";
// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React as a runtime value (classic transform)
import React, { useRef } from "react";
import type { ApplyResult } from "../../../code/edit-blocks.js";
import type { EditMode } from "../../../config.js";
import { t } from "../../../i18n/index.js";
import type { JobRegistry } from "../../../tools/jobs.js";
import { Card } from "../primitives/Card.js";
import { CardHeader } from "../primitives/CardHeader.js";
import { PILL_MODEL, PILL_SECTION, Pill, modelBadgeFor } from "../primitives/Pill.js";
import { PULSE_CIRCLE, PULSE_HEX, PULSE_SQUARE, Pulse } from "../primitives/Pulse.js";
import { useAgentState } from "../state/provider.js";
import { useThemeTokens } from "../theme/context.js";
import { CARD, FG, TONE } from "../theme/tokens.js";
import { useSlowTick } from "../ticker.js";
import type { SubagentActivity } from "../useSubagent.js";

/** Estimate output tokens accumulated since the last user submit (current turn). Includes both settled and in-flight reasoning/streaming cards so the counter doesn't reset while a tool runs mid-turn. ~4 chars/token, no tokenizer call. Two-pass primitive selectors keep useSyncExternalStore snapshots reference-stable. */
function useLiveOutputTokens(): { tokens: number; tps: number | null } {
  const chars = useAgentState((s) => {
    let lastUserIdx = -1;
    for (let i = s.cards.length - 1; i >= 0; i--) {
      if (s.cards[i]!.kind === "user") {
        lastUserIdx = i;
        break;
      }
    }
    if (lastUserIdx < 0) return 0;
    let n = 0;
    for (let i = lastUserIdx + 1; i < s.cards.length; i++) {
      const c = s.cards[i]!;
      if (c.kind === "reasoning" || c.kind === "streaming") n += c.text.length;
    }
    return n;
  });
  const startedAt = useAgentState((s) => {
    let lastUserIdx = -1;
    for (let i = s.cards.length - 1; i >= 0; i--) {
      if (s.cards[i]!.kind === "user") {
        lastUserIdx = i;
        break;
      }
    }
    return lastUserIdx >= 0 ? s.cards[lastUserIdx]!.ts : 0;
  });
  useSlowTick();
  const tokens = Math.ceil(chars / 4);
  if (tokens < 4 || startedAt === 0) return { tokens, tps: null };
  const elapsedSec = (Date.now() - startedAt) / 1000;
  if (elapsedSec < 0.5) return { tokens, tps: null };
  return { tokens, tps: Math.round(tokens / elapsedSec) };
}

/** Elapsed seconds since first mount of the given key. Reset when key changes. */
function useElapsedSinceKey(key: string): number {
  const startRef = useRef<{ key: string; at: number }>({ key, at: Date.now() });
  if (startRef.current.key !== key) startRef.current = { key, at: Date.now() };
  useSlowTick();
  return Math.floor((Date.now() - startRef.current.at) / 1000);
}

function formatElapsed(secs: number): string {
  if (secs < 60) return `${secs}s`;
  const m = Math.floor(secs / 60);
  const s = secs % 60;
  return `${m}m${s.toString().padStart(2, "0")}s`;
}

/** "Thinking" row — italic label (model wait, not tool call). */
export function ThinkingRow({ text }: { text: string }) {
  const secs = useElapsedSinceKey(text);
  const { tokens, tps } = useLiveOutputTokens();
  const tail: string[] = [];
  if (secs >= 1) tail.push(formatElapsed(secs));
  if (tokens > 0) tail.push(`↓ ${tokens.toLocaleString()} tok`);
  if (tps !== null) tail.push(`${tps} t/s`);
  return (
    <Box marginY={1} paddingX={1} gap={1}>
      <Pulse active frames={PULSE_CIRCLE} settled="●" color={TONE.accent} />
      <Text italic color={FG.sub}>
        {text}
      </Text>
      {tail.length > 0 ? <Text color={FG.faint}>{`· ${tail.join(" · ")}`}</Text> : null}
    </Box>
  );
}

/** Bottom mode bar above PromptInput; plan-mode pill takes precedence over edit-mode. */
export function ModeStatusBar({
  editMode,
  pendingCount,
  flash,
  planMode,
  undoArmed,
  jobs,
}: {
  editMode: EditMode;
  pendingCount: number;
  flash: boolean;
  planMode: boolean;
  undoArmed: boolean;
  jobs?: JobRegistry;
}) {
  useSlowTick();
  const running = jobs?.runningCount() ?? 0;
  const jobsTag =
    running > 0 ? (
      <Text color={TONE.warn} bold>{`  ·  ⏵ ${running} job${running === 1 ? "" : "s"}`}</Text>
    ) : null;
  if (planMode) {
    return (
      <ModeBarFrame>
        <ModePill label={t("editMode.plan")} color={TONE.err} flash={flash} />
        <Text color={FG.faint}>{t("editMode.writesGated")}</Text>
        {jobsTag}
      </ModeBarFrame>
    );
  }
  const label =
    editMode === "yolo"
      ? t("editMode.yolo")
      : editMode === "auto"
        ? t("editMode.auto")
        : t("editMode.review");
  const pillColor = editMode === "yolo" ? TONE.err : editMode === "auto" ? TONE.accent : TONE.brand;
  const mid =
    editMode === "yolo"
      ? t("editMode.editsShellAuto")
      : editMode === "auto"
        ? t("editMode.editsLandNow")
        : pendingCount > 0
          ? t("editMode.queuedApplyDiscard", { count: pendingCount })
          : t("editMode.editsQueued");
  return (
    <ModeBarFrame>
      <ModePill label={label} color={pillColor} flash={flash} />
      <Text color={FG.faint}>{t("editMode.shiftTabFlip", { mid })}</Text>
      {jobsTag}
    </ModeBarFrame>
  );
}

function ModeBarFrame({ children }: { children: React.ReactNode }) {
  return <Box paddingX={1}>{children}</Box>;
}

function ModePill({
  label,
  color,
  flash,
}: {
  label: string;
  color: string;
  flash: boolean;
}) {
  return (
    <Text color={color} bold inverse={flash}>
      {`[${label}]`}
    </Text>
  );
}

/** Auto-mode "applied N edits — u to undo" banner; auto-hides via parent's setTimeout. */
export function UndoBanner({
  banner,
}: {
  banner: { results: ApplyResult[]; expiresAt: number; pausedRemainingMs: number | null };
}) {
  const paused = banner.pausedRemainingMs !== null;
  const ok = banner.results.filter((r) => r.status === "applied" || r.status === "created").length;
  const total = banner.results.length;
  return (
    <Box marginY={1} paddingX={1}>
      <Text backgroundColor={TONE.accent} color="black" bold>
        {` ✓ AUTO-APPLIED ${ok}/${total} `}
      </Text>
      <Text color={FG.faint}>{"   press "}</Text>
      <Text backgroundColor={TONE.brand} color="black" bold>
        {" u "}
      </Text>
      <Text color={FG.faint}>{" to undo · "}</Text>
      <Text backgroundColor={paused ? TONE.warn : FG.faint} color="black" bold>
        {" space "}
      </Text>
      <Text color={FG.faint}>{paused ? " to resume" : " to pause"}</Text>
      {paused ? (
        <Text color={TONE.warn} bold>
          {"  · paused"}
        </Text>
      ) : null}
    </Box>
  );
}

function subagentPhaseLabel(
  phase: "exploring" | "summarising" | undefined,
  iter: number,
  elapsedMs: number,
): string {
  if (phase === "summarising") return "summarising findings…";
  if (iter === 0 && elapsedMs < 2000) return "exploring task…";
  if (iter === 0) return "thinking…";
  return "working through tools…";
}

function subagentTitle(skillName: string | undefined, task: string): string {
  if (skillName) return `Sub-agent · ${skillName}`;
  const short = task.length > 32 ? `${task.slice(0, 32)}…` : task;
  return `Sub-agent · ${short || "anonymous"}`;
}

/** Live block for a single in-flight subagent — rich layout, used when only one is running. */
export function SubagentRow({ activity }: { activity: SubagentActivity }) {
  const seconds = (activity.elapsedMs / 1000).toFixed(1);
  const phase = subagentPhaseLabel(activity.phase, activity.iter, activity.elapsedMs);
  const last = activity.lastInner;
  const subtitle = activity.skillName ?? truncate(activity.task, 48);
  const modelBadge = activity.model ? modelBadgeFor(activity.model) : null;
  const streamLine = formatStreamLine(activity);
  return (
    <Card tone={CARD.subagent.color}>
      <CardHeader
        glyph={<Pulse active frames={PULSE_HEX} settled="⌬" color={CARD.subagent.color} />}
        tone={CARD.subagent.color}
        title="subagent"
        subtitle={subtitle}
        meta={[`iter ${activity.iter}`, `${seconds}s`]}
        right={
          modelBadge ? (
            <Pill label={modelBadge.label} {...PILL_MODEL[modelBadge.kind]} bold={false} />
          ) : null
        }
      />
      <Text color={FG.faint}>
        {"task  "}
        <Text color={FG.sub}>{activity.task}</Text>
      </Text>
      <Text color={FG.faint}>
        {"last  "}
        {last ? (
          <>
            <Text color={last.color}>{`${last.glyph} `}</Text>
            <Text color={FG.body}>{last.label}</Text>
            {last.meta ? <Text color={FG.faint}>{`   ${last.meta}`}</Text> : null}
          </>
        ) : (
          <Text color={FG.faint}>{t("editMode.queuedDots")}</Text>
        )}
      </Text>
      {streamLine ? (
        <Text color={FG.faint}>
          {"flow  "}
          <Text color={FG.sub}>{streamLine}</Text>
        </Text>
      ) : null}
      <Text color={TONE.brand}>
        {"▶  "}
        {phase}
      </Text>
    </Card>
  );
}

function formatBytes(n: number): string {
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
  return `${(n / (1024 * 1024)).toFixed(2)} MB`;
}

/** Same shape as formatStreamLine but designed for inline use inside OngoingToolRow — returns null when nothing has flowed yet. */
function formatSubagentBytes(a: SubagentActivity): string | null {
  if (a.outputChars + a.reasoningChars + a.toolReadChars === 0) return null;
  const parts: string[] = [];
  if (a.toolReadChars > 0) parts.push(`↓ ${formatBytes(a.toolReadChars)} read`);
  if (a.outputChars > 0) parts.push(`↑ ${formatBytes(a.outputChars)} out`);
  if (a.reasoningChars > 0) parts.push(`◆ ${formatBytes(a.reasoningChars)} think`);
  return parts.join(" · ");
}

/** null → no flow yet (avoid printing a 0 B line that looks like noise). */
function formatStreamLine(a: SubagentActivity): string | null {
  if (a.outputChars + a.reasoningChars + a.toolReadChars === 0) return null;
  const parts: string[] = [];
  // Read first — that's usually the dominant traffic for explore/research
  // and the most reassuring "files are being pulled in" signal.
  if (a.toolReadChars > 0) parts.push(`↓ read ${formatBytes(a.toolReadChars)}`);
  if (a.outputChars > 0) parts.push(`↑ out ${formatBytes(a.outputChars)}`);
  if (a.reasoningChars > 0) parts.push(`◆ think ${formatBytes(a.reasoningChars)}`);
  return parts.join(" · ");
}

/** 1 → rich; 2-max → compact rows; >max → compact + "+N more" fold. */
export function SubagentLiveStack({
  activities,
  max = 3,
}: {
  activities: ReadonlyArray<SubagentActivity>;
  max?: number;
}) {
  if (activities.length === 0) return null;
  if (activities.length === 1) return <SubagentRow activity={activities[0]!} />;
  const visible = activities.slice(0, max);
  const overflow = activities.length - visible.length;
  const summarising = activities.filter((a) => a.phase === "summarising").length;
  const metaParts = [`${activities.length} running`];
  if (summarising > 0) metaParts.push(`${summarising} summarising`);
  return (
    <Card tone={CARD.subagent.color}>
      <CardHeader
        glyph={<Pulse active frames={PULSE_HEX} settled="⌬" color={CARD.subagent.color} />}
        tone={CARD.subagent.color}
        title="subagents"
        subtitle={metaParts.join(" · ")}
      />
      {visible.map((a) => (
        <CompactSubagentLine key={a.runId} activity={a} />
      ))}
      {overflow > 0 ? <Text color={FG.faint}>{`  +${overflow} more running…`}</Text> : null}
    </Card>
  );
}

function CompactSubagentLine({ activity }: { activity: SubagentActivity }) {
  const summarising = activity.phase === "summarising";
  const glyphColor = summarising ? TONE.brand : CARD.subagent.color;
  const seconds = (activity.elapsedMs / 1000).toFixed(1).padStart(5);
  const title = activity.skillName ?? truncate(activity.task, 28);
  const titlePadded = title.padEnd(28);
  const last = activity.lastInner;
  return (
    <Box flexDirection="row">
      <Text>{"  "}</Text>
      <Pulse active frames={PULSE_HEX} settled={summarising ? "▶" : "⌬"} color={glyphColor} />
      <Text> </Text>
      <Text color={FG.body}>{titlePadded}</Text>
      <Text color={FG.faint}>{`  iter ${String(activity.iter).padStart(2)} · ${seconds}s · `}</Text>
      {last ? (
        <>
          <Text color={last.color}>{`${last.glyph} `}</Text>
          <Text color={FG.body}>{truncate(last.label, 18)}</Text>
          {last.meta ? <Text color={FG.faint}>{`  ${last.meta}`}</Text> : null}
        </>
      ) : (
        <Text color={FG.faint}>{t("editMode.queuedDots")}</Text>
      )}
    </Box>
  );
}

function truncate(text: string, max: number): string {
  return text.length > max ? `${text.slice(0, max)}…` : text;
}

const SUBAGENT_WRAPPER_TOOLS = new Set<string>([
  "explore",
  "research",
  "review",
  "security_review",
  "run_skill",
]);

/** Live spinner + arg summary while a tool call is in flight; absorbs MCP progress frames. Also surfaces subagent byte counters for subagent-shaped tools, so the row stays informative even if `SubagentLiveStack` is off-screen. */
export function OngoingToolRow({
  tool,
  progress,
  subagentActivities = [],
}: {
  tool: { name: string; args?: string };
  progress: { progress: number; total?: number; message?: string } | null;
  subagentActivities?: ReadonlyArray<SubagentActivity>;
}) {
  const summary = summarizeToolArgs(tool.name, tool.args);
  const argsBytes = tool.args ? tool.args.length : 0;
  const secs = useElapsedSinceKey(tool.name);
  const { tokens, tps } = useLiveOutputTokens();
  // For subagent-shaped wrappers, surface the live byte counters inline
  // so the user sees data flowing even if the rich SubagentRow isn't
  // visible (off-screen, race-condition, whatever). At most one subagent
  // is in flight per ongoingTool today — pick the freshest.
  const subagentBytes = SUBAGENT_WRAPPER_TOOLS.has(tool.name)
    ? subagentActivities[subagentActivities.length - 1]
    : undefined;
  const subagentBytesLine = subagentBytes ? formatSubagentBytes(subagentBytes) : null;
  const tailParts: string[] = [];
  if (argsBytes > 0) tailParts.push(`args ${formatBytes(argsBytes)}`);
  if (secs >= 1) tailParts.push(formatElapsed(secs));
  if (tokens > 0) tailParts.push(`↓ ${tokens.toLocaleString()} tok`);
  if (tps !== null) tailParts.push(`${tps} t/s`);
  return (
    <Box marginY={1} flexDirection="column" paddingX={1}>
      <Box gap={1}>
        <Pulse active frames={PULSE_SQUARE} settled="▣" color={CARD.tool.color} />
        <Text color={CARD.tool.color} bold>
          {tool.name}
        </Text>
        <Text color={FG.faint}>
          {"running"}
          {tailParts.length > 0 ? ` · ${tailParts.join(" · ")}` : ""}
        </Text>
      </Box>
      {subagentBytesLine ? (
        <Box paddingLeft={3}>
          <Text color={FG.faint}>{subagentBytesLine}</Text>
        </Box>
      ) : null}
      {progress ? (
        <Box paddingLeft={3}>
          <Text color={TONE.brand}>{renderProgressLine(progress)}</Text>
        </Box>
      ) : null}
      {summary ? (
        <Box paddingLeft={3}>
          <Text color={FG.faint}>{summary}</Text>
        </Box>
      ) : null}
    </Box>
  );
}

/** With `total`: bar + "n/total pct%". Without: "progress: n" + optional message. */
function renderProgressLine(p: { progress: number; total?: number; message?: string }): string {
  const msg = p.message ? `  ${p.message}` : "";
  if (p.total && p.total > 0) {
    const ratio = Math.max(0, Math.min(1, p.progress / p.total));
    const width = 20;
    const filled = Math.round(ratio * width);
    const bar = "█".repeat(filled) + "░".repeat(width - filled);
    const pct = (ratio * 100).toFixed(0);
    return `[${bar}] ${p.progress}/${p.total} ${pct}%${msg}`;
  }
  return `progress: ${p.progress}${msg}`;
}

/** Match on suffix (e.g. `_read_file`) — MCP bridge prepends server namespace. */
function summarizeToolArgs(name: string, args?: string): string {
  if (!args || args === "{}") return "";
  let parsed: Record<string, unknown>;
  try {
    parsed = JSON.parse(args) as Record<string, unknown>;
  } catch {
    return args.length > 80 ? `${args.slice(0, 80)}…` : args;
  }
  const hasSuffix = (s: string) => name === s || name.endsWith(`_${s}`);
  const path = typeof parsed.path === "string" ? parsed.path : undefined;
  if (hasSuffix("read_file")) {
    const head = typeof parsed.head === "number" ? `, head=${parsed.head}` : "";
    const tail = typeof parsed.tail === "number" ? `, tail=${parsed.tail}` : "";
    return `path: ${path ?? "?"}${head}${tail}`;
  }
  if (hasSuffix("write_file")) {
    const content = typeof parsed.content === "string" ? parsed.content : "";
    return `path: ${path ?? "?"} (${content.length} chars)`;
  }
  if (hasSuffix("edit_file")) {
    const edits = Array.isArray(parsed.edits) ? parsed.edits.length : 0;
    return `path: ${path ?? "?"} (${edits} edit${edits === 1 ? "" : "s"})`;
  }
  if (hasSuffix("list_directory") || hasSuffix("directory_tree")) {
    return `path: ${path ?? "?"}`;
  }
  if (hasSuffix("search_files")) {
    const pattern = typeof parsed.pattern === "string" ? parsed.pattern : "?";
    return `path: ${path ?? "?"} · pattern: ${pattern}`;
  }
  if (hasSuffix("move_file")) {
    const src = typeof parsed.source === "string" ? parsed.source : "?";
    const dst = typeof parsed.destination === "string" ? parsed.destination : "?";
    return `${src} → ${dst}`;
  }
  if (hasSuffix("get_file_info")) {
    return `path: ${path ?? "?"}`;
  }
  return args.length > 80 ? `${args.slice(0, 80)}…` : args;
}
