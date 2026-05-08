import { Box, Text } from "ink";
// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React as a runtime value (classic transform)
import React from "react";
import type { ApplyResult } from "../../../code/edit-blocks.js";
import type { EditMode } from "../../../config.js";
import type { JobRegistry } from "../../../tools/jobs.js";
import { CharBar } from "../char-bar.js";
import { Card } from "../primitives/Card.js";
import { CardHeader } from "../primitives/CardHeader.js";
import { PILL_MODEL, PILL_SECTION, Pill, modelBadgeFor } from "../primitives/Pill.js";
import { Spinner } from "../primitives/Spinner.js";
import { CARD, FG, TONE } from "../theme/tokens.js";
import { useElapsedSeconds, useSlowTick, useTick } from "../ticker.js";
import type { SubagentActivity } from "../useSubagent.js";

export const SPINNER_FRAMES = ["⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"];

/** "Thinking" row — soft pulse + italic label (model wait, not tool call). */
export function ThinkingRow({ text }: { text: string }) {
  const elapsed = useElapsedSeconds();
  return (
    <Box marginY={1} paddingX={1} gap={1}>
      <Spinner kind="circle" color={TONE.accent} />
      <Text italic color={FG.sub}>
        {text}
      </Text>
      <Text color={FG.faint}>{`${elapsed}s`}</Text>
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
        <ModePill label="PLAN MODE" color={TONE.err} flash={flash} />
        <Text color={FG.faint}>{"   writes gated · /plan off to leave"}</Text>
        {jobsTag}
      </ModeBarFrame>
    );
  }
  const label = editMode === "yolo" ? "YOLO" : editMode === "auto" ? "AUTO" : "REVIEW";
  const pillColor = editMode === "yolo" ? TONE.err : editMode === "auto" ? TONE.accent : TONE.brand;
  const mid =
    editMode === "yolo"
      ? "edits + shell auto · /undo to roll back"
      : editMode === "auto"
        ? "edits land now · u to undo"
        : pendingCount > 0
          ? `${pendingCount} queued · y apply · n discard`
          : "edits queued · y apply · n discard";
  return (
    <ModeBarFrame>
      <ModePill label={label} color={pillColor} flash={flash} />
      <Text color={FG.faint}>{`   ${mid} · Shift+Tab to flip`}</Text>
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

/** Auto-mode "applied N edits — u to undo" banner; cleanup in parent's setTimeout. */
export function UndoBanner({
  banner,
}: {
  banner: { results: ApplyResult[]; expiresAt: number; pausedRemainingMs: number | null };
}) {
  useTick();
  const totalMs = 5000;
  const paused = banner.pausedRemainingMs !== null;
  const remainingMs = paused
    ? (banner.pausedRemainingMs ?? 0)
    : Math.max(0, banner.expiresAt - Date.now());
  const remainingSec = Math.ceil(remainingMs / 1000);
  const ok = banner.results.filter((r) => r.status === "applied" || r.status === "created").length;
  const total = banner.results.length;
  const urgent = !paused && remainingSec <= 1;
  const pct = (remainingMs / totalMs) * 100;
  const tone = paused ? TONE.warn : urgent ? TONE.err : TONE.accent;
  return (
    <Box marginY={1} paddingX={1}>
      <Text backgroundColor={TONE.accent} color="black" bold>
        {` ✓ AUTO-APPLIED ${ok}/${total} `}
      </Text>
      <Text color={FG.faint}>{"   press "}</Text>
      <Text backgroundColor={TONE.brand} color="black" bold>
        {" u "}
      </Text>
      <Text color={FG.faint}>{paused ? " to undo · " : " to undo · "}</Text>
      <Text backgroundColor={paused ? TONE.warn : FG.faint} color="black" bold>
        {" space "}
      </Text>
      <Text color={FG.faint}>{paused ? " to resume  " : " to pause  "}</Text>
      <CharBar pct={pct} width={20} color={tone} showLabel={false} />
      <Text color={FG.faint}>{"  "}</Text>
      <Text color={tone} bold={urgent || paused}>
        {paused ? `${remainingSec}s · paused` : `${remainingSec}s`}
      </Text>
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
  useTick();
  const seconds = (activity.elapsedMs / 1000).toFixed(1);
  const phase = subagentPhaseLabel(activity.phase, activity.iter, activity.elapsedMs);
  const last = activity.lastInner;
  const subtitle = activity.skillName ?? truncate(activity.task, 48);
  const modelBadge = activity.model ? modelBadgeFor(activity.model) : null;
  return (
    <Card tone={CARD.subagent.color}>
      <CardHeader
        glyph="⌬"
        tone={CARD.subagent.color}
        title="subagent"
        titleColor={PILL_SECTION.plan.fg}
        titleBg={PILL_SECTION.plan.bg}
        subtitle={subtitle}
        meta={[`iter ${activity.iter}`, `${seconds}s`]}
        right={
          <>
            {modelBadge ? (
              <Pill label={modelBadge.label} {...PILL_MODEL[modelBadge.kind]} bold={false} />
            ) : null}
            <Spinner kind="braille" color={CARD.subagent.color} />
          </>
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
          <Text color={FG.faint}>queued…</Text>
        )}
      </Text>
      <Text color={TONE.brand}>
        {"▶  "}
        {phase}
      </Text>
    </Card>
  );
}

/** 1 → rich; 2-max → compact rows; >max → compact + "+N more" fold. */
export function SubagentLiveStack({
  activities,
  max = 3,
}: {
  activities: ReadonlyArray<SubagentActivity>;
  max?: number;
}) {
  const tick = useTick();
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
        glyph="⌬"
        tone={CARD.subagent.color}
        title="subagents"
        titleColor={PILL_SECTION.plan.fg}
        titleBg={PILL_SECTION.plan.bg}
        subtitle={metaParts.join(" · ")}
        right={<Spinner kind="braille" color={CARD.subagent.color} />}
      />
      {visible.map((a, i) => (
        <CompactSubagentLine key={a.runId} activity={a} tick={tick} index={i} />
      ))}
      {overflow > 0 ? <Text color={FG.faint}>{`  +${overflow} more running…`}</Text> : null}
    </Card>
  );
}

function CompactSubagentLine({
  activity,
  tick,
  index,
}: {
  activity: SubagentActivity;
  tick: number;
  index: number;
}) {
  const summarising = activity.phase === "summarising";
  const spinnerFrame = SPINNER_FRAMES[(tick + index) % SPINNER_FRAMES.length] ?? "·";
  const glyph = summarising ? "▶" : spinnerFrame;
  const glyphColor = summarising ? TONE.brand : CARD.subagent.color;
  const seconds = (activity.elapsedMs / 1000).toFixed(1).padStart(5);
  const title = activity.skillName ?? truncate(activity.task, 28);
  const titlePadded = title.padEnd(28);
  const last = activity.lastInner;
  return (
    <Box flexDirection="row">
      <Text color={glyphColor} bold>
        {`  ${glyph} `}
      </Text>
      <Text color={FG.body}>{titlePadded}</Text>
      <Text color={FG.faint}>{`  iter ${String(activity.iter).padStart(2)} · ${seconds}s · `}</Text>
      {last ? (
        <>
          <Text color={last.color}>{`${last.glyph} `}</Text>
          <Text color={FG.body}>{truncate(last.label, 18)}</Text>
          {last.meta ? <Text color={FG.faint}>{`  ${last.meta}`}</Text> : null}
        </>
      ) : (
        <Text color={FG.faint}>queued…</Text>
      )}
    </Box>
  );
}

function truncate(text: string, max: number): string {
  return text.length > max ? `${text.slice(0, max)}…` : text;
}

/** Live spinner + arg summary while a tool call is in flight; absorbs MCP progress frames. */
export function OngoingToolRow({
  tool,
  progress,
}: {
  tool: { name: string; args?: string };
  progress: { progress: number; total?: number; message?: string } | null;
}) {
  const tick = useTick();
  const elapsed = useElapsedSeconds();
  const summary = summarizeToolArgs(tool.name, tool.args);
  return (
    <Box marginY={1} flexDirection="column" paddingX={1}>
      <Box>
        <Text color={CARD.tool.color} bold>
          {SPINNER_FRAMES[tick % SPINNER_FRAMES.length]}
        </Text>
        <Text>{"  "}</Text>
        <Text color={CARD.tool.color} bold>
          {`▣ ${tool.name}`}
        </Text>
        <Text color={FG.faint}>{`  running · ${elapsed}s`}</Text>
      </Box>
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
