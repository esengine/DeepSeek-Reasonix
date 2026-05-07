/** Pass order: scavenge → truncation → storm. Schema flatten runs at loop construction, not per-turn. */

import type { ToolCall } from "../types.js";
import { scavengeToolCalls } from "./scavenge.js";
import { type IsMutating, type IsStormExempt, StormBreaker } from "./storm.js";
import { repairTruncatedJson } from "./truncation.js";

export { analyzeSchema, flattenSchema, nestArguments } from "./flatten.js";
export type { FlattenDecision } from "./flatten.js";
export { repairTruncatedJson } from "./truncation.js";
export type { TruncationRepairResult } from "./truncation.js";
export { scavengeToolCalls } from "./scavenge.js";
export type { ScavengeOptions, ScavengeResult } from "./scavenge.js";
export { StormBreaker } from "./storm.js";

export interface RepairReport {
  scavenged: number;
  truncationsFixed: number;
  stormsBroken: number;
  notes: string[];
}

export interface ToolCallRepairOptions {
  allowedToolNames: ReadonlySet<string>;
  stormWindow?: number;
  stormThreshold?: number;
  maxScavenge?: number;
  /** Mutating calls clear the storm window so a post-edit verify-read isn't seen as a repeat. */
  isMutating?: IsMutating;
  /** Cheap state-inspection calls that should never trip repeat-loop suppression. */
  isStormExempt?: IsStormExempt;
}

export class ToolCallRepair {
  private readonly storm: StormBreaker;
  private readonly opts: ToolCallRepairOptions;

  constructor(opts: ToolCallRepairOptions) {
    this.opts = opts;
    this.storm = new StormBreaker(
      opts.stormWindow ?? 6,
      opts.stormThreshold ?? 3,
      opts.isMutating,
      opts.isStormExempt,
    );
  }

  /** Called at start of every user turn — fresh intent shouldn't inherit old repetition state. */
  resetStorm(): void {
    this.storm.reset();
  }

  process(
    declaredCalls: ToolCall[],
    reasoningContent: string | null,
    content: string | null = null,
  ): { calls: ToolCall[]; report: RepairReport } {
    const report: RepairReport = {
      scavenged: 0,
      truncationsFixed: 0,
      stormsBroken: 0,
      notes: [],
    };

    // 1. Scavenge — only add calls whose (name,args) signature is novel.
    // Scan both channels: reasoning (where R1 leaks JSON calls into
    // <think>) AND content (where it emits DSML markup in regular
    // turns). Joined with a newline so the scanners see the blobs as
    // independent bodies. Dedup below keeps us from inflating if the
    // same call shows up in both — first seen wins.
    const combined = [reasoningContent ?? "", content ?? ""].filter(Boolean).join("\n");
    const scavenged = scavengeToolCalls(combined || null, {
      allowedNames: this.opts.allowedToolNames,
      maxCalls: this.opts.maxScavenge ?? 4,
    });
    const seenSignatures = new Set(declaredCalls.map(signature));
    const merged = [...declaredCalls];
    for (const sc of scavenged.calls) {
      if (!seenSignatures.has(signature(sc))) {
        merged.push(sc);
        report.scavenged++;
        seenSignatures.add(signature(sc));
      }
    }
    report.notes.push(...scavenged.notes);

    // 2. Truncation repair on argument JSON.
    for (const call of merged) {
      const args = call.function?.arguments ?? "";
      const r = repairTruncatedJson(args);
      if (r.changed) {
        call.function.arguments = r.repaired;
        report.truncationsFixed++;
        report.notes.push(...r.notes.map((n) => `[${call.function.name}] ${n}`));
      }
    }

    // 3. Storm breaker.
    const filtered: ToolCall[] = [];
    for (const call of merged) {
      const verdict = this.storm.inspect(call);
      if (verdict.suppress) {
        report.stormsBroken++;
        if (verdict.reason) report.notes.push(verdict.reason);
        continue;
      }
      filtered.push(call);
    }

    return { calls: filtered, report };
  }
}

function signature(call: ToolCall): string {
  return `${call.function?.name ?? ""}::${call.function?.arguments ?? ""}`;
}
