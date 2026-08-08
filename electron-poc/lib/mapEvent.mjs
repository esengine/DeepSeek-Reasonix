/**
 * Map serve SSE JSON payloads into the desktop wire event shape.
 *
 * Serve broadcaster emits eventwire.ToWire JSON: top-level `kind` (not `type`),
 * with nested `approval` / `ask` / `tool` objects. Preserve that contract so
 * useController-style consumers work without inventing alternate keys.
 *
 * @param {unknown} raw
 * @returns {Record<string, unknown> | null}
 */
export function mapServeEventToWire(raw) {
  if (raw == null) return null;
  let obj = raw;
  if (typeof raw === "string") {
    const s = raw.trim();
    if (!s) return null;
    try {
      obj = JSON.parse(s);
    } catch {
      // Non-JSON SSE data: surface as notice without inventing a false kind name
      // for structured events. Plain text is rare on the serve stream.
      return { kind: "notice", text: s, level: "info", _transport: "http-sse" };
    }
  }
  if (typeof obj !== "object" || obj === null || Array.isArray(obj)) {
    return { kind: "notice", text: String(obj), level: "info", _transport: "http-sse" };
  }
  // Shallow clone only — nested approval/ask/tool objects stay intact.
  const e = /** @type {Record<string, unknown>} */ ({ ...obj });

  // Preserve kind. Never force type:"unknown" or invent type aliases.
  // If a payload already has kind, leave it. Missing kind is left missing so
  // callers can detect malformed frames rather than mis-routing as "unknown".
  if (typeof e.kind === "string") {
    e.kind = e.kind; // keep
  }

  // Real edge cases only: drop accidental dual type/kind inventing nothing.
  // Some older mocks used `type`; if kind is absent but type is a known wire
  // kind name, promote type → kind without inventing new names.
  if ((e.kind == null || e.kind === "") && typeof e.type === "string" && e.type) {
    const known = new Set([
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
    if (known.has(e.type)) {
      e.kind = e.type;
    }
  }

  e._transport = "http-sse";
  return e;
}

/**
 * Parse an SSE buffer chunk stream into data payloads.
 * Handles multi-line data: fields and ignores comment lines.
 * @param {string} chunk
 * @param {{ pending?: string }} state mutable parse state
 * @returns {string[]} complete data payload strings
 */
export function parseSseChunk(chunk, state) {
  state.pending = (state.pending ?? "") + chunk;
  const events = [];
  let idx;
  while ((idx = state.pending.indexOf("\n\n")) >= 0) {
    const block = state.pending.slice(0, idx);
    state.pending = state.pending.slice(idx + 2);
    const dataLines = [];
    for (const line of block.split(/\r?\n/)) {
      if (line.startsWith(":")) continue; // comment / keepalive
      if (line.startsWith("data:")) {
        dataLines.push(line.slice(5).replace(/^ /, ""));
      }
    }
    if (dataLines.length) events.push(dataLines.join("\n"));
  }
  return events;
}
