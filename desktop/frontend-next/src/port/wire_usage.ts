import type { Money } from "./wire";

// Only prefixChangeReasons is omitempty on the wire; the rest always arrive, so
// marking them optional would make every reader handle an absence that the
// producer never sends.
export interface CacheDiagnostics {
  prefixHash: string;
  prefixChanged: boolean;
  prefixChangeReasons?: string[];
  toolSchemaTokens: number;
  cacheMissTokens: number;
  cacheHitTokens: number;
  // Whether the messages this request carried over are the bytes the last one
  // sent. A miss with neither the prefix nor the body changed is the
  // provider's, and the panel has to be able to say so.
  carriedMessages: number;
  bodyChanged: boolean;
  bodyHash: string;
}

// How much of the usage behind a quote carries a complete price fact. A single
// call is only ever complete or incomplete; an aggregate has two more answers,
// because it can price some of its calls and not others, and it can have billed
// nothing at all. Kept apart from displayStatus, which is about whether the
// requested currency could be shown, not about whether the cost is known.
export type CostCoverage = "none" | "complete" | "partial" | "incomplete";

export interface CostQuote {
  original: Money;
  selected?: Money;
  estimated: boolean;
  costComplete: boolean;
  // Absent from a kernel older than the field — a remote workspace can be one.
  coverage?: CostCoverage;
  // Why the cost fact is short, kept as provenance. The state is read from
  // coverage, never worked backwards out of this.
  incompleteReason?: string;
}

export interface Usage {
  promptTokens: number;
  completionTokens: number;
  totalTokens: number;
  cacheHitTokens: number;
  cacheMissTokens: number;
  reasoningTokens?: number;
  estimated?: boolean;
  source?: string;
  // The stream attempt that billed these tokens. Usage lands after its round
  // has settled, so this is what attaches it — not whatever is still open.
  attemptId?: string;
  cacheDiagnostics?: CacheDiagnostics;
  sessionCacheHitTokens: number;
  sessionCacheMissTokens: number;
  // Context* is the latest single request's shape, for gauges and rebind. Absent
  // means fall back to the billable prompt/completion totals above.
  contextPromptTokens?: number;
  contextCompletionTokens?: number;
  contextReasoningTokens?: number;
  contextCacheHitTokens?: number;
  contextCacheMissTokens?: number;
  cost?: number;
  currency?: string;
  currencyCode?: string;
  costQuote?: CostQuote;
}
