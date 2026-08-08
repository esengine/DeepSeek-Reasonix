/**
 * Map serve SSE JSON into the desktop WireEvent shape (eventwire contract).
 * Serve emits top-level `kind` with nested approval/ask/tool — preserve them.
 */

const KNOWN_KINDS = new Set([
  "turn_started",
  "reasoning",
  "text",
  "message",
  "tool_dispatch",
  "tool_result",
  "usage",
  "notice",
  "phase",
  "approval_request",
  "ask_request",
  "turn_done",
  "compaction_started",
  "compaction_done",
  "tool_progress",
  "retrying",
  "steer",
  "stream_attempt",
]);

export function mapServeEventToWire(raw: unknown): Record<string, unknown> | null {
  if (raw == null) return null;
  let obj: unknown = raw;
  if (typeof raw === "string") {
    const s = raw.trim();
    if (!s) return null;
    try {
      obj = JSON.parse(s);
    } catch {
      return { kind: "notice", text: s, level: "info", _transport: "http-sse" };
    }
  }
  if (typeof obj !== "object" || obj === null || Array.isArray(obj)) {
    return { kind: "notice", text: String(obj), level: "info", _transport: "http-sse" };
  }
  const e: Record<string, unknown> = { ...(obj as Record<string, unknown>) };

  // Preserve kind. Do not invent type or force type:"unknown".
  if ((e.kind == null || e.kind === "") && typeof e.type === "string" && e.type) {
    if (KNOWN_KINDS.has(e.type)) {
      e.kind = e.type;
    }
  }

  e._transport = "http-sse";
  return e;
}

export function parseSseChunk(
  chunk: string,
  state: { pending?: string },
): string[] {
  state.pending = (state.pending ?? "") + chunk;
  const events: string[] = [];
  let idx: number;
  while ((idx = state.pending.indexOf("\n\n")) >= 0) {
    const block = state.pending.slice(0, idx);
    state.pending = state.pending.slice(idx + 2);
    const dataLines: string[] = [];
    for (const line of block.split(/\r?\n/)) {
      if (line.startsWith(":")) continue;
      if (line.startsWith("data:")) dataLines.push(line.slice(5).replace(/^ /, ""));
    }
    if (dataLines.length) events.push(dataLines.join("\n"));
  }
  return events;
}
