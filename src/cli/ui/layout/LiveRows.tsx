import { Box, Text } from "ink";
// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React as a runtime value (classic transform)
import React from "react";
import type { ApplyResult } from "../../../code/edit-blocks.js";
import type { EditMode } from "../../../config.js";
import type { JobRegistry } from "../../../tools/jobs.js";
import { CharBar } from "../char-bar.js";
import { Spinner } from "../primitives/Spinner.js";
import { CARD, FG, TONE } from "../theme/tokens.js";
import { useElapsedSeconds, useSlowTick, useTick } from "../ticker.js";

export const SPINNER_FRAMES = ["⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"];

/** "Thinking" row — circle spinner per design (model wait, not tool call). */
export function ThinkingRow({ text }: { text: string }) {
  const elapsed = useElapsedSeconds();
  return (
    <Box marginY={1} paddingX={1}>
      <Spinner kind="circle" color={TONE.brand} bold />
      <Text>{"  "}</Text>
      <Text color={TONE.brand}>{text}</Text>
      <Text color={FG.faint}>{`  ·  ${elapsed}s`}</Text>
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

const DASHED_RULE_STYLE = {
  topLeft: "╌",
  top: "╌",
  topRight: "╌",
  bottomLeft: " ",
  bottom: " ",
  bottomRight: " ",
  left: " ",
  right: " ",
} as const;

function ModeBarFrame({ children }: { children: React.ReactNode }) {
  return (
    <Box flexDirection="column">
      <Box
        paddingX={1}
        borderStyle={DASHED_RULE_STYLE}
        borderTop
        borderRight={false}
        borderBottom={false}
        borderLeft={false}
        borderTopColor={FG.faint}
      />
      <Box paddingX={1}>{children}</Box>
    </Box>
  );
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
  banner: { results: ApplyResult[]; expiresAt: number };
}) {
  useTick();
  const totalMs = 5000;
  const remainingMs = Math.max(0, banner.expiresAt - Date.now());
  const remainingSec = Math.ceil(remainingMs / 1000);
  const ok = banner.results.filter((r) => r.status === "applied" || r.status === "created").length;
  const total = banner.results.length;
  const urgent = remainingSec <= 1;
  const pct = (remainingMs / totalMs) * 100;
  const tone = urgent ? TONE.err : TONE.accent;
  return (
    <Box marginY={1} paddingX={1}>
      <Text backgroundColor={TONE.accent} color="black" bold>
        {` ✓ AUTO-APPLIED ${ok}/${total} `}
      </Text>
      <Text color={FG.faint}>{"   press "}</Text>
      <Text backgroundColor={TONE.brand} color="black" bold>
        {" u "}
      </Text>
      <Text color={FG.faint}>{" to undo  "}</Text>
      <CharBar pct={pct} width={20} color={tone} showLabel={false} />
      <Text color={FG.faint}>{"  "}</Text>
      <Text color={tone} bold={urgent}>
        {`${remainingSec}s`}
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

/** Live block for a running subagent. Fixed row count — never grows as inner events arrive, so the screen doesn't jump. */
export function SubagentRow({
  activity,
}: {
  activity: {
    task: string;
    iter: number;
    elapsedMs: number;
    skillName?: string;
    model?: string;
    phase?: "exploring" | "summarising";
    lastInner: { glyph: string; color: string; label: string; meta?: string } | null;
  };
}) {
  const tick = useTick();
  const seconds = (activity.elapsedMs / 1000).toFixed(1);
  const phase = subagentPhaseLabel(activity.phase, activity.iter, activity.elapsedMs);
  const last = activity.lastInner;
  return (
    <Box flexDirection="column" marginY={1}>
      <Box>
        <Text>{"  "}</Text>
        <Text color={CARD.subagent.color}>{"▎ "}</Text>
        <Text bold color={CARD.subagent.color}>
          {`⌬ ${subagentTitle(activity.skillName, activity.task)}`}
        </Text>
        <Box flexGrow={1} />
        <Text color={CARD.subagent.color}>{`iter ${activity.iter} · ${seconds}s`}</Text>
        <Text>{"  "}</Text>
        <Text color={CARD.subagent.color} bold>
          {SPINNER_FRAMES[tick % SPINNER_FRAMES.length]}
        </Text>
      </Box>
      <Box>
        <Text>{"  "}</Text>
        <Text color={CARD.subagent.color}>{"▎"}</Text>
      </Box>
      <Box>
        <Text>{"  "}</Text>
        <Text color={CARD.subagent.color}>{"▎   "}</Text>
        <Text color={FG.faint}>{"Task   "}</Text>
        <Text color={FG.sub}>{activity.task}</Text>
      </Box>
      <Box>
        <Text>{"  "}</Text>
        <Text color={CARD.subagent.color}>{"▎   "}</Text>
        <Text color={FG.faint}>{"Model  "}</Text>
        <Text color={FG.sub}>{activity.model ?? "—"}</Text>
      </Box>
      <Box>
        <Text>{"  "}</Text>
        <Text color={CARD.subagent.color}>{"▎"}</Text>
      </Box>
      <Box>
        <Text>{"  "}</Text>
        <Text color={CARD.subagent.color}>{"▎   "}</Text>
        <Text color={FG.faint}>{"Last   "}</Text>
        {last ? (
          <>
            <Text color={last.color}>{`${last.glyph} `}</Text>
            <Text color={FG.body}>{last.label}</Text>
            {last.meta ? <Text color={FG.faint}>{`   ${last.meta}`}</Text> : null}
          </>
        ) : (
          <Text color={FG.faint}>{"queued…"}</Text>
        )}
      </Box>
      <Box>
        <Text>{"  "}</Text>
        <Text color={CARD.subagent.color}>{"▎"}</Text>
      </Box>
      <Box>
        <Text>{"  "}</Text>
        <Text color={CARD.subagent.color}>{"▎   "}</Text>
        <Text bold color={TONE.brand}>
          {"▶ "}
        </Text>
        <Text color={TONE.brand}>{phase}</Text>
      </Box>
    </Box>
  );
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
