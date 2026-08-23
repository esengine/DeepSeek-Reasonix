import type { ResolvedReasoningDisplayMode } from "./reasoningDisplayPreference";
import { estimateTranscriptTextHeight } from "./transcriptRowEstimates";
import type { TranscriptRow, ToolItem } from "./transcriptRows";

/**
 * The rendered geometry state of one virtual transcript row. Every producer
 * (row model, DOM disclosure, estimate cache, and snapshot validator) uses this
 * vocabulary so a measured expanded body can never seed a collapsed row.
 */
export type TranscriptRowLayoutVariant =
  | "reasoning-summary"
  | "reasoning-heading-only"
  | "reasoning-expanded"
  | "tool-collapsed"
  | "tool-expanded"
  | "tool-batch-collapsed"
  | "tool-batch-expanded"
  | "tool-group-collapsed"
  | "tool-group-expanded"
  | "compaction-collapsed"
  | "compaction-expanded"
  | "static"
  | "text-flow";

export type TranscriptEstimateSource = "exact" | "compact-median" | "calibrated" | "static";

export type TranscriptGeometryEnvironment = {
  /** Actual readable transcript column, not the native window width. */
  contentWidth?: number;
  /** Content/code/metadata font families, sizes, and line heights. */
  typographySignature: string;
};

const TRANSCRIPT_LAYOUT_VARIANTS: readonly TranscriptRowLayoutVariant[] = [
  "reasoning-summary", "reasoning-heading-only", "reasoning-expanded",
  "tool-collapsed", "tool-expanded", "tool-batch-collapsed", "tool-batch-expanded",
  "tool-group-collapsed", "tool-group-expanded", "compaction-collapsed",
  "compaction-expanded", "static", "text-flow",
];

export function isTranscriptRowLayoutVariant(value: unknown): value is TranscriptRowLayoutVariant {
  return typeof value === "string" && TRANSCRIPT_LAYOUT_VARIANTS.includes(value as TranscriptRowLayoutVariant);
}

export const TRANSCRIPT_COMPACT_HEIGHTS = {
  "reasoning-summary": 64,
  "reasoning-heading-only": 36,
  "tool-collapsed": 37,
  "tool-batch-collapsed": 32,
  "tool-group-collapsed": 32,
  "compaction-collapsed": 36,
} as const satisfies Partial<Record<TranscriptRowLayoutVariant, number>>;

const EXPANDED_TEXT_HEIGHT_CAP = 6_000;

export function resolveReasoningLayoutVariant(
  mode: ResolvedReasoningDisplayMode,
  running: boolean,
): Extract<TranscriptRowLayoutVariant, `reasoning-${string}`> | null {
  if (mode === "hidden" || mode === "pending") return null;
  if (mode === "expanded" || (mode === "auto" && running)) return "reasoning-expanded";
  if (mode === "legacy-collapsed") return "reasoning-heading-only";
  return "reasoning-summary";
}

/** Shared default-open policy used by both ToolCard and the virtual row model. */
export function resolveToolCardDefaultOpen(
  item: ToolItem,
  nestedCount: number,
  reasoningDisplayMode: ResolvedReasoningDisplayMode,
): boolean {
  const subagentReasoningRunning = item.subagentProgress?.phase === "reasoning";
  const liveFollow = reasoningDisplayMode === "auto" || reasoningDisplayMode === "expanded";
  const keepSubagentReasoningExpanded = reasoningDisplayMode === "expanded" && Boolean(item.subagentProgress?.reasoning);
  return (nestedCount > 0 && item.status === "running")
    || (liveFollow && subagentReasoningRunning)
    || keepSubagentReasoningExpanded;
}

export function isStableCompactTranscriptVariant(
  variant: TranscriptRowLayoutVariant,
): variant is keyof typeof TRANSCRIPT_COMPACT_HEIGHTS {
  return Object.prototype.hasOwnProperty.call(TRANSCRIPT_COMPACT_HEIGHTS, variant);
}

export function transcriptRowLayoutVariant(row: TranscriptRow): TranscriptRowLayoutVariant {
  if (row.layoutVariant) return row.layoutVariant;
  switch (row.kind) {
    case "reasoning": return "reasoning-summary";
    case "tool": return "tool-collapsed";
    case "tool-batch": return "tool-batch-collapsed";
    case "tool-group": return "tool-group-collapsed";
    case "compaction": return row.item.pending ? "static" : "compaction-collapsed";
    case "user":
    case "answer":
    case "notice":
    case "extension":
      return "text-flow";
    default:
      return "static";
  }
}

function lineColumnsForWidth(contentWidth: number | undefined): number {
  if (!Number.isFinite(contentWidth) || (contentWidth ?? 0) <= 0) return 88;
  // The established 88-column estimator corresponds to the 960px content
  // column. Clamp narrow windows and ultra-wide layouts to useful bounds.
  return Math.max(28, Math.min(160, Math.round((contentWidth! / 960) * 88)));
}

function textHeight(text: string | undefined, minimum: number, environment: TranscriptGeometryEnvironment): number {
  return estimateTranscriptTextHeight(text, minimum, {
    lineColumns: lineColumnsForWidth(environment.contentWidth),
    maximum: EXPANDED_TEXT_HEIGHT_CAP,
  });
}

function answerTextHeight(text: string | undefined, environment: TranscriptGeometryEnvironment): number {
  // For ordinary Markdown, explicit blank source lines are replaced by the
  // renderer's 10–12px block margins, which tracks the EAW source estimate
  // closely. Fenced code replaces three source lines (open/body/close) with a
  // padded code box, so it needs a separate structural conversion. These
  // constants are browser-contract baselines, not hidden-content guesses.
  const sourceHeight = textHeight(text, 58, environment);
  if (!text?.includes("```")) return Math.max(58, sourceHeight - 7);
  return Math.max(58, Math.round(36 + (sourceHeight - 44) * 0.4 + 60));
}

function userTextHeight(text: string | undefined, environment: TranscriptGeometryEnvironment): number {
  const transcriptColumns = lineColumnsForWidth(environment.contentWidth);
  // User bubbles occupy at most 82% of the readable column and reserve 32px
  // horizontal padding, so using the full answer width undercounts CJK wraps.
  const bubbleColumns = Math.max(18, Math.floor(transcriptColumns * 0.82) - 6);
  return estimateTranscriptTextHeight(text, 56, {
    lineColumns: bubbleColumns,
    maximum: EXPANDED_TEXT_HEIGHT_CAP,
  }) + 23;
}

function toolText(item: ToolItem): string {
  return [item.args, item.output, item.error, item.subagentProgress?.reasoning, item.subagentProgress?.text]
    .filter(Boolean)
    .join("\n");
}

/** State-aware initial geometry seed. It never reads hidden collapsed bodies. */
export function estimateTranscriptRowGeometry(
  row: TranscriptRow | undefined,
  environment: TranscriptGeometryEnvironment,
): number {
  if (!row) return 48;
  const variant = transcriptRowLayoutVariant(row);
  if (isStableCompactTranscriptVariant(variant)) return TRANSCRIPT_COMPACT_HEIGHTS[variant];

  switch (variant) {
    case "reasoning-expanded":
      return row.kind === "reasoning" ? textHeight(row.item.reasoning, 96, environment) : 96;
    case "tool-expanded":
      return row.kind === "tool" ? textHeight(toolText(row.item), 96, environment) : 96;
    case "tool-batch-expanded":
    case "tool-group-expanded": {
      if (row.kind !== "tool-batch" && row.kind !== "tool-group") return 96;
      const total = row.items.reduce((height, item) => height + textHeight(toolText(item), 48, environment), 32);
      return Math.min(EXPANDED_TEXT_HEIGHT_CAP, total);
    }
    case "compaction-expanded":
      return row.kind === "compaction" ? textHeight(row.item.summary, 72, environment) : 72;
    case "text-flow":
      switch (row.kind) {
        case "user": return userTextHeight(row.item.text, environment);
        case "answer": return answerTextHeight(row.item.text, environment);
        case "notice": return textHeight(`${row.item.text}\n${row.item.detail ?? ""}`, 44, environment);
        case "extension": return 112;
        default: return 48;
      }
    case "static":
      switch (row.kind) {
        case "older-history": return 44;
        case "process-header": return 28;
        case "phase": return 28;
        case "process-notice": return 44;
        case "compaction": return 36;
        case "turn-actions": return 26;
        default: return 48;
      }
    default:
      return 48;
  }
}
