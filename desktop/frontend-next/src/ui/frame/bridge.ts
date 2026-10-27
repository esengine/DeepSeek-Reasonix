import type { Slot } from "../slots";
import { fitsJSON, fitsString, isRecord } from "./json";

export const FRAME_PROTOCOL = "reasonix.frame.v1";

/** One message a frame document may send its host. Nothing else crosses: a
 *  frame cannot submit a prompt, answer an approval, change a setting or open
 *  a URL, because no message here does any of those. */
export type FrameMessage =
  | { type: "ready" }
  | { type: "size"; height: number }
  | { type: "action"; actionId: string; args?: Record<string, string> }
  | { type: "submit"; values: Record<string, unknown> }
  | { type: "state"; value: unknown };

const ACTION_ID = /^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$/;
const MAX_ARG_PAIRS = 16;
const MAX_ARG_BYTES = 1024;
const MAX_SUBMIT_BYTES = 64 * 1024;
const MAX_STATE_BYTES = 16 * 1024;

const only = (v: Record<string, unknown>, ...keys: string[]) => Object.keys(v).every((k) => keys.includes(k));

function args(v: unknown): Record<string, string> | null {
  if (!isRecord(v)) return null;
  const entries = Object.entries(v);
  if (entries.length > MAX_ARG_PAIRS) return null;
  for (const [k, value] of entries) {
    if (typeof value !== "string" || !fitsString(k, MAX_ARG_BYTES) || !fitsString(value, MAX_ARG_BYTES)) return null;
  }
  return v as Record<string, string>;
}

/** decodeFrameMessage reads one message strictly: an unknown type, an extra
 *  field or a value past its bound is null, never a best guess. */
export function decodeFrameMessage(data: unknown): FrameMessage | null {
  if (!isRecord(data) || typeof data.type !== "string") return null;
  switch (data.type) {
    case "ready":
      return only(data, "type") ? { type: "ready" } : null;
    case "size":
      return only(data, "type", "height") && typeof data.height === "number" && Number.isFinite(data.height) && data.height >= 0
        ? { type: "size", height: data.height }
        : null;
    case "action": {
      if (!only(data, "type", "actionId", "args") || typeof data.actionId !== "string" || !ACTION_ID.test(data.actionId)) return null;
      if (data.args === undefined) return { type: "action", actionId: data.actionId };
      const a = args(data.args);
      return a ? { type: "action", actionId: data.actionId, args: a } : null;
    }
    case "submit":
      return only(data, "type", "values") && isRecord(data.values) && fitsJSON(data.values, MAX_SUBMIT_BYTES)
        ? { type: "submit", values: data.values }
        : null;
    case "state":
      return only(data, "type", "value") && "value" in data && fitsJSON(data.value, MAX_STATE_BYTES) ? { type: "state", value: data.value } : null;
    default:
      return null;
  }
}

const PER_SECOND: Record<FrameMessage["type"], number> = { ready: 1, size: 10, action: 5, submit: 1, state: 10 };

/** RateGate holds each message kind to its rate over a sliding second. */
export class RateGate {
  private seen = new Map<FrameMessage["type"], number[]>();
  allow(type: FrameMessage["type"], now: number): boolean {
    const recent = (this.seen.get(type) ?? []).filter((t) => now - t < 1000);
    if (recent.length >= PER_SECOND[type]) {
      this.seen.set(type, recent);
      return false;
    }
    recent.push(now);
    this.seen.set(type, recent);
    return true;
  }
}

/** DropCounter says when a frame has sent enough refused messages to be closed. */
export class DropCounter {
  private drops: number[] = [];
  record(now: number): boolean {
    this.drops = [...this.drops.filter((t) => now - t < 10_000), now];
    return this.drops.length >= 3;
  }
}

/** The height range each place gives a frame; a tab fills its pane. */
export const SLOT_HEIGHTS: Record<Slot, { min: number; max: number } | null> = {
  sidebar: { min: 120, max: 640 },
  "composer-trailing": { min: 32, max: 160 },
  "session-header": null,
  "pane-tab": null,
};

export function clampHeight(height: number, range: { min: number; max: number } | null): number | undefined {
  return range ? Math.min(range.max, Math.max(range.min, Math.round(height))) : undefined;
}

/** The semantic tokens a frame is handed, read from the page as resolved values. */
export const FRAME_TOKENS = [
  "--text", "--muted", "--faint", "--page", "--surface", "--raised", "--border", "--hair",
  "--accent", "--accent-fg", "--ok", "--warn", "--err", "--link", "--focus", "--ui", "--mono", "--r-sm", "--r-md",
] as const;

export function themeTokens(): Record<string, string> {
  const style = getComputedStyle(document.documentElement);
  return Object.fromEntries(FRAME_TOKENS.map((name) => [name, style.getPropertyValue(name).trim()]));
}
