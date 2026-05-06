/** R1 sometimes emits tool-call JSON inside reasoning_content and forgets `tool_calls`; recover those calls. */

import type { ToolCall } from "../types.js";

export interface ScavengeOptions {
  /** Names of tools the model may legitimately call. Other names are ignored. */
  allowedNames: ReadonlySet<string>;
  /** Maximum number of calls to scavenge per pass (defence against runaway). */
  maxCalls?: number;
}

export interface ScavengeResult {
  calls: ToolCall[];
  notes: string[];
}

/** Bounds the regex input — DSML matchers are O(n²) on adversarial input per CodeQL js/polynomial-redos. */
const MAX_SCAVENGE_INPUT = 100 * 1024;

export function scavengeToolCalls(
  reasoningContent: string | null | undefined,
  opts: ScavengeOptions,
): ScavengeResult {
  if (!reasoningContent) return { calls: [], notes: [] };
  if (reasoningContent.length > MAX_SCAVENGE_INPUT) {
    return {
      calls: [],
      notes: [`scavenge skipped: reasoning_content too large (${reasoningContent.length} chars)`],
    };
  }
  const max = opts.maxCalls ?? 4;
  const notes: string[] = [];
  const out: ToolCall[] = [];

  // Pattern A: DSML invoke blocks. R1 sometimes emits tool calls as
  // its chat-template markup in the content channel instead of the
  // proper `tool_calls` field. 0.4.3 stripped these from display;
  // here we actually turn them back into proper ToolCalls so the
  // model's intent isn't lost.
  for (const invoke of iterateDsmlInvokes(reasoningContent)) {
    if (out.length >= max) break;
    if (!opts.allowedNames.has(invoke.name)) continue;
    out.push({
      function: {
        name: invoke.name,
        arguments: JSON.stringify(invoke.args),
      },
    });
    notes.push(`scavenged DSML call: ${invoke.name}`);
  }

  // Pattern B: raw JSON objects (the original three shapes). Strip
  // any DSML blocks we already processed so parameter JSON buried
  // inside them doesn't get re-scavenged as a standalone call.
  const nonDsml = stripDsmlBlocks(reasoningContent);
  for (const candidate of iterateJsonObjects(nonDsml)) {
    if (out.length >= max) break;
    const call = coerceToToolCall(candidate, opts.allowedNames);
    if (call) {
      out.push(call);
      notes.push(`scavenged call: ${call.function.name}`);
    }
  }
  return { calls: out, notes };
}

interface DsmlInvoke {
  name: string;
  args: Record<string, unknown>;
}

/** Strips DSML invoke blocks so the raw-JSON scanner doesn't re-scavenge their parameter payloads. */
function stripDsmlBlocks(text: string): string {
  let out = text;
  out = out.replace(/<[｜|]DSML[｜|]function_calls>[\s\S]*?<\/?[｜|]DSML[｜|]function_calls>/g, "");
  out = out.replace(/<[｜|]DSML[｜|]invoke\s+[^>]*>[\s\S]*?<\/[｜|]DSML[｜|]invoke>/g, "");
  return out;
}

function* iterateDsmlInvokes(text: string): Generator<DsmlInvoke> {
  // `｜` (U+FF5C) in practice; `|` (ASCII) as a fallback seen in a
  // minority of builds. `[｜|]` inside the regex covers both.
  const INVOKE_RE = /<[｜|]DSML[｜|]invoke\s+name="([^"]+)">([\s\S]*?)<\/[｜|]DSML[｜|]invoke>/g;
  for (const match of text.matchAll(INVOKE_RE)) {
    const name = match[1];
    const body = match[2];
    if (!name || body === undefined) continue;
    yield { name, args: parseDsmlParameters(body) };
  }
}

/** Falls back to literal text when `string="false"` JSON parse fails — never lose the parameter. */
function parseDsmlParameters(body: string): Record<string, unknown> {
  const PARAM_RE =
    /<[｜|]DSML[｜|]parameter\s+name="([^"]+)"(?:\s+string="(true|false)")?\s*>([\s\S]*?)<\/[｜|]DSML[｜|]parameter>/g;
  const args: Record<string, unknown> = {};
  for (const m of body.matchAll(PARAM_RE)) {
    const key = m[1];
    const stringFlag = m[2];
    const raw = (m[3] ?? "").trim();
    if (!key) continue;
    if (stringFlag === "false") {
      try {
        args[key] = JSON.parse(raw);
        continue;
      } catch {
        // Fall through — keep as literal so the information isn't lost.
      }
    }
    args[key] = raw;
  }
  return args;
}

/** Yield every top-level JSON object substring in `text`. */
function* iterateJsonObjects(text: string): Generator<string> {
  for (let i = 0; i < text.length; i++) {
    if (text[i] !== "{") continue;
    let depth = 0;
    let inString = false;
    let escaped = false;
    for (let j = i; j < text.length; j++) {
      const c = text[j]!;
      if (escaped) {
        escaped = false;
        continue;
      }
      if (inString) {
        if (c === "\\") {
          escaped = true;
          continue;
        }
        if (c === '"') inString = false;
        continue;
      }
      if (c === '"') inString = true;
      else if (c === "{") depth++;
      else if (c === "}") {
        depth--;
        if (depth === 0) {
          yield text.slice(i, j + 1);
          i = j;
          break;
        }
      }
    }
  }
}

function coerceToToolCall(
  candidateJson: string,
  allowedNames: ReadonlySet<string>,
): ToolCall | null {
  let parsed: any;
  try {
    parsed = JSON.parse(candidateJson);
  } catch {
    return null;
  }
  if (!parsed || typeof parsed !== "object") return null;

  // Pattern 1: { name, arguments }
  if (typeof parsed.name === "string" && allowedNames.has(parsed.name)) {
    const args = parsed.arguments;
    return {
      function: {
        name: parsed.name,
        arguments: typeof args === "string" ? args : JSON.stringify(args ?? {}),
      },
    };
  }

  // Pattern 2: OpenAI-style { type: "function", function: { name, arguments } }
  if (
    parsed.type === "function" &&
    parsed.function &&
    typeof parsed.function.name === "string" &&
    allowedNames.has(parsed.function.name)
  ) {
    const args = parsed.function.arguments;
    return {
      type: "function",
      function: {
        name: parsed.function.name,
        arguments: typeof args === "string" ? args : JSON.stringify(args ?? {}),
      },
    };
  }

  // Pattern 3: { tool_name, tool_args } (R1 free-form variant)
  if (typeof parsed.tool_name === "string" && allowedNames.has(parsed.tool_name)) {
    return {
      function: {
        name: parsed.tool_name,
        arguments: JSON.stringify(parsed.tool_args ?? {}),
      },
    };
  }

  return null;
}
