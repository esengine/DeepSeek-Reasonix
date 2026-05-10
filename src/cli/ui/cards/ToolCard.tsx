import { Box, Text, useStdout } from "ink";
// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React from "react";
import { clipToCells } from "../../../frame/width.js";
import { t } from "../../../i18n/index.js";
import { Markdown } from "../markdown.js";
import { Card } from "../primitives/Card.js";
import { CardHeader, type MetaItem } from "../primitives/CardHeader.js";
import { Spinner } from "../primitives/Spinner.js";
import type { ToolCard as ToolCardData } from "../state/cards.js";
import { useIsInflight } from "../state/inflight-context.js";
import { FG, TONE, TONE_ACTIVE } from "../theme/tokens.js";

const READ_TAIL = 2;
const OTHER_TAIL = 5;

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
  const lineCells = Math.max(20, cols - 4);
  const argsLabel = formatArgsSummary(card.args);

  const subagentMarkdown = unwrapSubagentMarkdown(card);

  const allLines = card.output.length > 0 ? card.output.split("\n") : [];
  const tail = tailLinesFor(card.name);
  const truncated = allLines.length > tail;
  const visible = truncated ? allLines.slice(-tail) : allLines;
  const hidden = truncated ? allLines.length - visible.length : 0;
  const isInflight = useIsInflight(card.id);
  const status = toolStatus(card, isInflight);
  const headColor = headerColorFor(status);
  const errColor = card.exitCode && card.exitCode !== 0 ? TONE.err : FG.sub;
  // Rejected calls show a single trailing badge — the verbose JSON error body
  // is already conveyed by the badge, so dropping the body keeps the card tight.
  const showBody = !card.rejected && (subagentMarkdown !== null || visible.length > 0);

  const meta: MetaItem[] = [];
  if (card.retry) {
    meta.push({ text: `↻ ${card.retry.attempt}/${card.retry.max}`, color: TONE.warn });
  }
  if (card.rejected) {
    meta.push({ text: t("cardLabels.rejected"), color: TONE.err });
  }
  for (const part of metaTrail(card)) meta.push(part);

  return (
    <Card tone={headColor}>
      <CardHeader
        glyph={statusGlyph(status)}
        tone={headColor}
        title={card.name}
        subtitle={argsLabel || undefined}
        meta={meta.length > 0 ? meta : undefined}
        right={
          status === "running" ? (
            <Spinner kind="braille" color={TONE_ACTIVE.brand} bold />
          ) : undefined
        }
      />
      {showBody &&
        (subagentMarkdown !== null ? (
          <Markdown text={subagentMarkdown} width={lineCells} />
        ) : (
          <>
            {hidden > 0 ? (
              <Text color={FG.faint}>
                {t(hidden === 1 ? "cardLabels.earlierLine" : "cardLabels.earlierLines", {
                  count: hidden,
                })}
              </Text>
            ) : null}
            {visible.map((line, i) => (
              <Text
                key={`${card.id}:${hidden + i}`}
                color={errColor}
                dimColor={!card.exitCode || card.exitCode === 0}
              >
                {clipToCells(line, lineCells) || " "}
              </Text>
            ))}
          </>
        ))}
    </Card>
  );
}

function unwrapSubagentMarkdown(card: ToolCardData): string | null {
  if (card.name !== "spawn_subagent") return null;
  if (card.output.length === 0) return null;
  try {
    const parsed = JSON.parse(card.output) as unknown;
    if (!parsed || typeof parsed !== "object") return null;
    const obj = parsed as Record<string, unknown>;
    if (obj.success !== true) return null;
    if (typeof obj.output !== "string") return null;
    return obj.output;
  } catch {
    return null;
  }
}

type ToolStatus = "running" | "ok" | "rejected" | "error" | "aborted";

function toolStatus(card: ToolCardData, isInflight: boolean): ToolStatus {
  // Running is derived from the loop's inflight set so a missed `tool` event
  // can't strand the spinner forever — finally in runOneToolCall guarantees
  // the id leaves the set on every exit path.
  if (isInflight) return "running";
  if (card.rejected) return "rejected";
  if (card.aborted) return "aborted";
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
      return TONE_ACTIVE.brand;
  }
}

function metaTrail(card: ToolCardData): string[] {
  const parts: string[] = [];
  const inputBytes = largestStringInputBytes(card.args);
  if (inputBytes !== null) parts.push(t("cardLabels.bytesIn", { bytes: formatBytes(inputBytes) }));
  if (card.elapsedMs > 0)
    parts.push(t("cardLabels.elapsedSec", { secs: (card.elapsedMs / 1000).toFixed(2) }));
  if (
    card.done &&
    !card.rejected &&
    !card.aborted &&
    card.exitCode !== undefined &&
    card.exitCode !== 0
  ) {
    parts.push(t("cardLabels.exit", { code: card.exitCode }));
  }
  return parts;
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
