import type { ModelEntry } from "../port/port";
import { accountKey, accountLabel, disambiguate } from "./vendors";

export interface Vendor {
  key: string;
  label: string;
  host: string;
  // The config entry's own name, shown only when one host holds two accounts.
  hint: string;
  // Models this account offers under each protocol it answers on.
  byKind: Record<string, ModelEntry[]>;
  kinds: string[];
}

export function groupVendors(models: ModelEntry[]): Vendor[] {
  const out = new Map<string, Vendor>();
  for (const m of models) {
    const host = m.vendor || m.provider || "";
    const key = accountKey(host, m.keyEnv);
    let v = out.get(key);
    if (!v) {
      v = { key, label: "", host, hint: m.provider, byKind: {}, kinds: [] };
      out.set(key, v);
    }
    const kind = m.kind || "openai";
    if (!v.byKind[kind]) {
      v.byKind[kind] = [];
      v.kinds.push(kind);
    }
    v.byKind[kind].push(m);
  }
  // The connection list names the same accounts from the same rule; a picker
  // calling one of them something else is two names for one thing.
  for (const v of out.values()) {
    const entries = Object.values(v.byKind).flat();
    v.label = accountLabel(v.host, entries.map((m) => ({ name: m.provider, preset: m.preset, displayName: m.displayName })));
  }
  return disambiguate([...out.values()]);
}

// Which protocol this account is on right now: the one holding the running
// model, else the one holding its default, else the first that answered.
export function activeKind(v: Vendor, current?: string): string {
  for (const kind of v.kinds) {
    if (v.byKind[kind].some((m) => m.ref === current)) return kind;
  }
  for (const kind of v.kinds) {
    if (v.byKind[kind].some((m) => m.default)) return kind;
  }
  return v.kinds[0];
}

export function contextLabel(tokens?: number): string {
  if (!tokens || tokens <= 0) return "";
  if (tokens >= 1_000_000) {
    const m = tokens / 1_000_000;
    return `${Number.isInteger(m) ? m : m.toFixed(1)}M`;
  }
  return `${Math.round(tokens / 1024)}K`;
}
