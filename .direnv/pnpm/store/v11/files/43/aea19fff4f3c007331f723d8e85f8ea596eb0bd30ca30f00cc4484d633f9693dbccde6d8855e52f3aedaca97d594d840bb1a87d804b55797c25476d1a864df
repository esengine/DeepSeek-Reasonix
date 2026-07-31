import type { ValidateLayoutResult } from '../layout-utils/validateLayout.js';
/**
 * One named entry to be folded into an aggregate report. The `id` shows up in
 * the `byCase` rows so a developer can spot the worst-scoring fixture or the
 * one that has gone invalid.
 */
export interface NamedValidateResult {
    id: string;
    result: ValidateLayoutResult;
}
/**
 * Aggregate snapshot over a batch of `validateLayout` runs. Designed to be
 * dumped via `console.table(report.byCase)` or asserted on
 * (`report.invalidCount === 0`) in a fixture-sweep spec.
 */
export interface AggregateValidateReport {
    /** Sum of `result.score` across all entries. Zero entries → 0. */
    totalScore: number;
    /** `totalScore / count`. Zero entries → 0. */
    avgScore: number;
    /** Smallest `result.score` across all entries. Zero entries → 0. */
    minScore: number;
    /** Number of entries with `result.ok === false`. */
    invalidCount: number;
    /** Per-fixture rows ordered as supplied. */
    byCase: {
        id: string;
        score: number;
        valid: boolean;
        issueTypes: string[];
    }[];
}
/**
 * Combine multiple {@link ValidateLayoutResult}s into a single
 * {@link AggregateValidateReport}.
 *
 * The helper is intentionally pure and side-effect free — callers decide
 * whether to log, assert, or print the report. Per-case `issueTypes` is the
 * deduplicated, sorted list of issue types so `console.table` rows stay
 * compact.
 */
export declare function combineValidateLayoutResults(items: NamedValidateResult[]): AggregateValidateReport;
