import { createHash } from "node:crypto";
import type { ChatMessage, ToolSpec } from "../types.js";

export interface ImmutablePrefixOptions {
  system: string;
  toolSpecs?: readonly ToolSpec[];
  fewShots?: readonly ChatMessage[];
}

export class ImmutablePrefix {
  /** Stable across turns; rebuilt only on /new when REASONIX.md changed on disk. */
  system: string;
  /** Each `addTool` costs one cache-miss turn — DeepSeek's prefix cache is keyed by full tool list. */
  private _toolSpecs: ToolSpec[];
  readonly fewShots: readonly ChatMessage[];
  /** Invalidated by addTool / removeTool / replaceSystem; bypassing any of those leaves cache stale → fingerprint diverges from sent prefix. */
  private _fingerprintCache: string | null = null;

  constructor(opts: ImmutablePrefixOptions) {
    this.system = opts.system;
    this._toolSpecs = [...(opts.toolSpecs ?? [])];
    this.fewShots = Object.freeze([...(opts.fewShots ?? [])]);
  }

  /** Replaces the system prompt; returns true iff the string actually changed. Caller must accept a cache miss on the next turn. */
  replaceSystem(s: string): boolean {
    if (this.system === s) return false;
    this.system = s;
    this._fingerprintCache = null;
    return true;
  }

  get toolSpecs(): readonly ToolSpec[] {
    return this._toolSpecs;
  }

  toMessages(): ChatMessage[] {
    return [{ role: "system", content: this.system }, ...this.fewShots.map((m) => ({ ...m }))];
  }

  tools(): ToolSpec[] {
    return this._toolSpecs.map((t) => structuredClone(t) as ToolSpec);
  }

  addTool(spec: ToolSpec): boolean {
    const name = spec.function?.name;
    if (!name) return false;
    if (this._toolSpecs.some((t) => t.function?.name === name)) return false;
    this._toolSpecs.push(spec);
    this._fingerprintCache = null;
    return true;
  }

  /** Mirror of addTool for MCP hot-unbridge. Same cache-miss cost — prefix changes shape. */
  removeTool(name: string): boolean {
    const idx = this._toolSpecs.findIndex((t) => t.function?.name === name);
    if (idx < 0) return false;
    this._toolSpecs.splice(idx, 1);
    this._fingerprintCache = null;
    return true;
  }

  get fingerprint(): string {
    if (this._fingerprintCache !== null) return this._fingerprintCache;
    this._fingerprintCache = this.computeFingerprint();
    return this._fingerprintCache;
  }

  /** Dev/test only — throws on cache drift, which always means a non-`addTool` mutation slipped in. */
  verifyFingerprint(): string {
    const fresh = this.computeFingerprint();
    if (this._fingerprintCache !== null && this._fingerprintCache !== fresh) {
      throw new Error(
        `ImmutablePrefix fingerprint drift: cached=${this._fingerprintCache}, fresh=${fresh}. A mutation path bypassed addTool's cache invalidation — DeepSeek will see prefix churn that the TUI / transcript log don't know about.`,
      );
    }
    this._fingerprintCache = fresh;
    return fresh;
  }

  private computeFingerprint(): string {
    const blob = JSON.stringify({
      system: this.system,
      tools: this._toolSpecs,
      shots: this.fewShots,
    });
    return createHash("sha256").update(blob).digest("hex").slice(0, 16);
  }
}

export class AppendOnlyLog {
  private _entries: ChatMessage[] = [];

  append(message: ChatMessage): void {
    if (!message || typeof message !== "object" || !("role" in message)) {
      throw new Error(`invalid log entry: ${JSON.stringify(message)}`);
    }
    this._entries.push(message);
  }

  extend(messages: ChatMessage[]): void {
    for (const m of messages) this.append(m);
  }

  /** The one append-only-breaking path — reserved for `/compact` + recovery. Use `append()` otherwise. */
  compactInPlace(replacement: ChatMessage[]): void {
    this._entries = [...replacement];
  }

  get entries(): readonly ChatMessage[] {
    return this._entries;
  }

  toMessages(): ChatMessage[] {
    return this._entries.map((e) => ({ ...e }));
  }

  get length(): number {
    return this._entries.length;
  }
}

export class VolatileScratch {
  reasoning: string | null = null;
  planState: Record<string, unknown> | null = null;
  notes: string[] = [];

  reset(): void {
    this.reasoning = null;
    this.planState = null;
    this.notes = [];
  }
}
