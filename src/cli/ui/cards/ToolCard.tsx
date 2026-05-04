import { Box, Text, useStdout } from "ink";
// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React from "react";
import { clipToCells } from "../../../frame/width.js";
import { Spinner } from "../primitives/Spinner.js";
import type { ToolCard as ToolCardData } from "../state/cards.js";
import { FG, TONE } from "../theme/tokens.js";

const READ_TAIL = 2;
const OTHER_TAIL = 5;
const BODY_PAD = 2;

/** Read-style tools dump file/list bodies — short tail is enough; the model already has the full text in context. */
function tailLinesFor(name: string): number {
  const lower = name.toLowerCase();
  return /(?:^|_)(read|search|list|tree|get|status|diff|fetch|grep)(_|$)/.test(lower) ||
    lower === "job_output"
    ? READ_TAIL
    : OTHER_TAIL;
}

export function ToolCard({ card }: { card: ToolCardData }): React.ReactElement {
  const { stdout } = useStdout();
  const cols = stdout?.columns ?? 80;
  const lineCells = Math.max(20, cols - BODY_PAD - 4);
  const argsLabel = formatArgsSummary(card.args);
  const allLines = card.output.length > 0 ? card.output.split("\n") : [];
  const tail = tailLinesFor(card.name);
  const truncated = allLines.length > tail;
  const visible = truncated ? allLines.slice(-tail) : allLines;
  const hidden = truncated ? allLines.length - visible.length : 0;
  const status = toolStatus(card);
  const headColor = headerColorFor(status);
  const errColor = card.exitCode && card.exitCode !== 0 ? TONE.err : FG.sub;
  // Rejected calls show a single trailing badge — the verbose JSON error body
  // is already conveyed by the badge, so dropping the body keeps the card tight.
  const showBody = !card.rejected && visible.length > 0;

  return (
    <Box flexDirection="column" marginTop={1}>
      <Box flexDirection="row" gap={1}>
        <Text color={headColor}>{statusGlyph(status)}</Text>
        <Text color={headColor} bold>
          {card.name}
        </Text>
        {argsLabel ? <Text color={FG.faint}>{argsLabel}</Text> : null}
        {card.retry ? (
          <Text color={TONE.warn} bold>{`↻ ${card.retry.attempt}/${card.retry.max}`}</Text>
        ) : null}
        {card.rejected ? (
          <Text color={TONE.err} bold>
            rejected
          </Text>
        ) : null}
        <Text color={FG.faint}>{metaTrail(card)}</Text>
        {status === "running" ? (
          <Box flexDirection="row">
            <Spinner kind="braille" color={TONE.brand} bold />
          </Box>
        ) : null}
      </Box>
      {showBody && (
        <>
          {hidden > 0 ? (
            <Box paddingLeft={BODY_PAD}>
              <Text color={FG.faint}>
                {`⋮ ${hidden} earlier line${hidden === 1 ? "" : "s"} (use /tool to read full)`}
              </Text>
            </Box>
          ) : null}
          {visible.map((line, i) => (
            <Box key={`${card.id}:${hidden + i}`} paddingLeft={BODY_PAD}>
              <Text color={errColor} dimColor={!card.exitCode || card.exitCode === 0}>
                {clipToCells(line, lineCells) || " "}
              </Text>
            </Box>
          ))}
        </>
      )}
    </Box>
  );
}

type ToolStatus = "running" | "ok" | "rejected" | "error" | "aborted";

function toolStatus(card: ToolCardData): ToolStatus {
  if (card.rejected) return "rejected";
  if (card.aborted) return "aborted";
  if (!card.done) return "running";
  if (card.exitCode !== undefined && card.exitCode !== 0) return "error";
  return "ok";
}

function statusGlyph(s: ToolStatus): string {
  switch (s) {
    case "running":
      return "▢";
    case "ok":
      return "✓";
    case "rejected":
      return "✗";
    case "error":
      return "✖";
    case "aborted":
      return "⊘";
  }
}

function headerColorFor(s: ToolStatus): string {
  switch (s) {
    case "ok":
      return TONE.ok;
    case "rejected":
    case "error":
    case "aborted":
      return TONE.err;
    case "running":
      return TONE.brand;
  }
}

function metaTrail(card: ToolCardData): string {
  const parts: string[] = [];
  const inputBytes = largestStringInputBytes(card.args);
  if (inputBytes !== null) parts.push(`${formatBytes(inputBytes)} in`);
  if (card.elapsedMs > 0) parts.push(`${(card.elapsedMs / 1000).toFixed(2)}s`);
  if (
    card.done &&
    !card.rejected &&
    !card.aborted &&
    card.exitCode !== undefined &&
    card.exitCode !== 0
  ) {
    parts.push(`exit ${card.exitCode}`);
  }
  return parts.length > 0 ? `· ${parts.join(" · ")}` : "";
}

function formatArgsSummary(args: unknown): string {
  if (typeof args === "string") return args.length > 60 ? `${args.slice(0, 60)}…` : args;
  if (args && typeof args === "object") {
    const keys = Object.keys(args as Record<string, unknown>);
    if (keys.length === 0) return "";
    const first = keys[0]!;
    const value = (args as Record<string, unknown>)[first];
    if (typeof value === "string") {
      const trimmed = value.length > 40 ? `${value.slice(0, 40)}…` : value;
      return keys.length === 1 ? trimmed : `${trimmed}  +${keys.length - 1}`;
    }
    return keys.join(" ");
  }
  return "";
}

const INPUT_SIZE_THRESHOLD = 1024;

/** Largest string field on args, when above threshold. Surfaces input bulk for write_file (content), edit_file (replace), run_command (long stdin), etc. without per-tool special cases. */
export function largestStringInputBytes(args: unknown): number | null {
  let max = 0;
  if (typeof args === "string") {
    max = args.length;
  } else if (args && typeof args === "object") {
    for (const v of Object.values(args as Record<string, unknown>)) {
      if (typeof v === "string" && v.length > max) max = v.length;
    }
  }
  return max >= INPUT_SIZE_THRESHOLD ? max : null;
}

function formatBytes(n: number): string {
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
  return `${(n / (1024 * 1024)).toFixed(1)} MB`;
}
