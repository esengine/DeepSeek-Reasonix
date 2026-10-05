const ARG_KEYS = ["command", "path", "file_path", "pattern", "query", "url", "description", "prompt", "step_id"];

export function shortArgs(raw: string) {
  if (!raw) return "";
  const pick = (o: unknown): string => {
    if (!o || typeof o !== "object") return "";
    const rec = o as Record<string, unknown>;
    for (const k of ARG_KEYS) {
      const v = rec[k];
      if (typeof v === "string" && v) return v.replace(/\s+/g, " ").slice(0, 96);
    }
    return "";
  };
  try {
    const v = JSON.parse(raw);
    // A stable proxy nests the real call under `arguments`, which its own
    // schema declares. Reading only the outer object describes the proxy.
    return pick(v) || pick((v as Record<string, unknown>)?.arguments);
  } catch {
    // A model occasionally emits JSON with an unescaped quote inside a string.
    // Spilling the first 96 characters of that is not "what this call touched",
    // it is a broken payload's innards pasted onto the card — so a thing that
    // was meant to be an object shows nothing rather than showing its guts.
    if (raw.trimStart().startsWith("{")) return "";
    return raw.replace(/\s+/g, " ").slice(0, 96);
  }
}

// GOAL_STATUS is the vocabulary update_goal is allowed to report, and each entry
// says what the model just claimed rather than naming the enum value.
export const GOAL_STATUS: Record<string, [string, string]> = {
  complete: ["宣告做完", "ok"],
  continue: ["还在做", "run"],
  blocked: ["卡住了，要你介入", "warn"],
};

// goalUpdate reads what update_goal was called with. It falls back to a regex
// because that call is exactly where malformed JSON shows up: the payload
// carries free-form prose the model wrote, quotes and all.
export function goalUpdate(raw: string | undefined): { status: string; reason: string } | null {
  if (!raw) return null;
  try {
    const v = JSON.parse(raw) as { status?: unknown; reason?: unknown };
    if (typeof v.status === "string") {
      return { status: v.status, reason: typeof v.reason === "string" ? v.reason : "" };
    }
  } catch {
    // fall through to the salvage below
  }
  const status = /"status"\s*:\s*"([a-z]+)"/.exec(raw)?.[1] ?? "";
  const reason = /"reason"\s*:\s*"((?:[^"\\]|\\.)*)"/.exec(raw)?.[1] ?? "";
  if (!status) return null;
  return { status, reason: reason.replace(/\\"/g, '"').replace(/\\n/g, " ") };
}

export function argOf(raw: string | undefined, ...keys: string[]): string {
  if (!raw) return "";
  try {
    const v = JSON.parse(raw);
    for (const k of keys) if (typeof v[k] === "string" && v[k]) return v[k];
  } catch {
    return "";
  }
  return "";
}

export function splitPath(p: string): [string, string] {
  const at = Math.max(p.lastIndexOf("/"), p.lastIndexOf("\\"));
  return at < 0 ? ["", p] : [p.slice(0, at + 1), p.slice(at + 1)];
}
