import type { SlashArgContext, SlashCommandSpec } from "./types.js";

export const SLASH_COMMANDS: readonly SlashCommandSpec[] = [
  { cmd: "help", group: "chat", summary: "show the full command reference", aliases: ["?"] },
  {
    cmd: "new",
    group: "chat",
    summary: "start a fresh conversation (clear context + scrollback)",
    aliases: ["reset", "clear"],
  },
  { cmd: "retry", group: "chat", summary: "truncate & resend your last message (fresh sample)" },
  {
    cmd: "compact",
    group: "chat",
    summary:
      "fold older turns into a summary message (cache-safe). Auto-fires at 50% ctx; this is the manual trigger.",
  },
  {
    cmd: "stop",
    group: "chat",
    summary: "abort the current model turn (typed alternative to Esc)",
  },

  {
    cmd: "preset",
    group: "setup",
    argsHint: "<auto|flash|pro>",
    summary: "model bundle — auto escalates flash → pro, flash/pro lock. Bare opens picker.",
    argCompleter: ["auto", "flash", "pro"],
  },
  {
    cmd: "model",
    group: "setup",
    argsHint: "<id>",
    summary: "switch DeepSeek model id. Bare opens picker.",
    argCompleter: "models",
  },

  { cmd: "status", group: "info", summary: "current model, flags, context, session" },
  {
    cmd: "cost",
    group: "info",
    argsHint: "[text]",
    summary:
      "bare → last turn's spend (Usage card); with text → estimate cost of sending it next (worst-case + likely-cache)",
  },
  {
    cmd: "context",
    group: "info",
    summary: "show context-window breakdown (system / tools / log / input)",
  },
  {
    cmd: "stats",
    group: "info",
    summary:
      "cross-session cost dashboard (today / week / month / all-time · cache hit · vs Claude)",
  },
  {
    cmd: "doctor",
    group: "info",
    summary: "health check (api / config / api-reach / index / hooks / project)",
  },

  { cmd: "sessions", group: "session", summary: "list saved sessions (current marked with ▸)" },

  { cmd: "mcp", group: "extend", summary: "list MCP servers + tools attached to this session" },
  {
    cmd: "resource",
    group: "extend",
    argsHint: "[uri]",
    summary: "browse + read MCP resources (no arg → list URIs; <uri> → fetch contents)",
    argCompleter: "mcp-resources",
  },
  {
    cmd: "prompt",
    group: "extend",
    argsHint: "[name]",
    summary: "browse + fetch MCP prompts (no arg → list names; <name> → render prompt)",
    argCompleter: "mcp-prompts",
  },
  {
    cmd: "memory",
    group: "extend",
    argsHint: "[list|show <name>|forget <name>|clear <scope> confirm]",
    summary: "show / manage pinned memory (REASONIX.md + ~/.reasonix/memory)",
  },
  {
    cmd: "skill",
    group: "extend",
    argsHint: "[list|show <name>|new <name>|<name> [args]]",
    summary: "list / run / scaffold user skills (<project>/.reasonix/skills + ~/.reasonix/skills)",
  },

  {
    cmd: "init",
    group: "code",
    argsHint: "[force]",
    summary:
      "scan the project and synthesize a baseline REASONIX.md (model writes; review with /apply). `force` overwrites an existing file.",
    contextual: "code",
    argCompleter: ["force"],
  },
  {
    cmd: "apply",
    group: "code",
    argsHint: "[N|N,M|N-M]",
    summary:
      "commit pending edit blocks to disk (no arg → all; `1`, `1,3`, or `1-4` → that subset, rest stay pending)",
    contextual: "code",
  },
  {
    cmd: "discard",
    group: "code",
    argsHint: "[N|N,M|N-M]",
    summary: "drop pending edit blocks without writing (no arg → all; indices → that subset)",
    contextual: "code",
  },
  {
    cmd: "walk",
    group: "code",
    summary:
      "step through pending edits one block at a time (git-add-p style: y/n per block, a apply rest, A flip AUTO)",
    contextual: "code",
  },
  {
    cmd: "undo",
    group: "code",
    summary: "roll back the last applied edit batch",
    contextual: "code",
  },
  {
    cmd: "history",
    group: "code",
    summary: "list every edit batch this session (ids for /show, undone markers)",
    contextual: "code",
  },
  {
    cmd: "show",
    group: "code",
    argsHint: "[id]",
    summary: "dump a stored edit diff (omit id for newest non-undone)",
    contextual: "code",
  },
  {
    cmd: "commit",
    group: "code",
    argsHint: '"msg"',
    summary: "git add -A && git commit -m ...",
    contextual: "code",
  },
  {
    cmd: "mode",
    group: "code",
    argsHint: "[review|auto|yolo]",
    summary:
      "edit-gate: review (queue) · auto (apply+undo) · yolo (apply+auto-shell). Shift+Tab cycles.",
    contextual: "code",
    argCompleter: ["review", "auto", "yolo"],
  },
  {
    cmd: "plan",
    group: "code",
    argsHint: "[on|off]",
    summary: "toggle read-only plan mode (writes bounced until submit_plan + approval)",
    contextual: "code",
    argCompleter: ["on", "off"],
  },
  {
    cmd: "checkpoint",
    group: "code",
    argsHint: "[name|list|forget <id>]",
    summary:
      "snapshot every file the session has touched (Cursor-style internal store, not git). /checkpoint alone lists.",
    contextual: "code",
    argCompleter: ["list", "forget"],
  },
  {
    cmd: "restore",
    group: "code",
    argsHint: "<name|id>",
    summary: "roll back files to a named checkpoint (see /checkpoint list)",
    contextual: "code",
  },

  {
    cmd: "jobs",
    group: "jobs",
    summary: "list background jobs started by run_background",
    contextual: "code",
  },
  {
    cmd: "kill",
    group: "jobs",
    argsHint: "<id>",
    summary: "stop a background job by id (SIGTERM → SIGKILL after grace)",
    contextual: "code",
  },
  {
    cmd: "logs",
    group: "jobs",
    argsHint: "<id> [lines]",
    summary: "tail a background job's output (default last 80 lines)",
    contextual: "code",
  },

  {
    cmd: "pro",
    group: "advanced",
    argsHint: "[off]",
    summary: "arm v4-pro for the NEXT turn only (one-shot · auto-disarms after turn)",
    argCompleter: ["off"],
  },
  {
    cmd: "budget",
    group: "advanced",
    argsHint: "[usd|off]",
    summary:
      "session USD cap — warns at 80%, refuses next turn at 100%. Off by default. /budget alone shows status",
    argCompleter: ["off", "1", "5", "10", "20", "50"],
  },
  {
    cmd: "language",
    group: "advanced",
    argsHint: "<EN|zh-CN>",
    summary: "switch the runtime language",
    argCompleter: ["EN", "zh-CN"],
    aliases: ["lang"],
  },
  {
    cmd: "search-engine",
    group: "advanced",
    argsHint: "<mojeek|searxng> [<endpoint>]",
    summary: "switch web search backend — mojeek (default, no deps) or searxng (self-hosted)",
    argCompleter: ["mojeek", "searxng"],
    aliases: ["se"],
  },
  {
    cmd: "hooks",
    group: "advanced",
    argsHint: "[reload]",
    summary: "list active hooks (settings.json under .reasonix/) · reload re-reads from disk",
  },
  {
    cmd: "permissions",
    group: "advanced",
    argsHint: "[list|add <prefix>|remove <prefix|N>|clear confirm]",
    summary:
      "show / edit shell allowlist (builtin read-only · per-project: ~/.reasonix/config.json)",
    argCompleter: ["list", "add", "remove", "clear"],
  },
  {
    cmd: "dashboard",
    group: "advanced",
    argsHint: "[stop]",
    summary: "launch the embedded web dashboard (127.0.0.1, token-gated)",
    argCompleter: ["stop"],
  },
  {
    cmd: "loop",
    group: "advanced",
    argsHint: "<5s..6h> <prompt>  ·  stop  ·  (no args = status)",
    summary: "auto-resubmit <prompt> every <interval> until you type something / Esc / /loop stop",
  },
  {
    cmd: "plans",
    group: "advanced",
    summary: "list this session's active + archived plans, newest first",
  },
  {
    cmd: "replay",
    group: "advanced",
    summary: "load an archived plan as a read-only Time Travel snapshot (default: newest)",
    argsHint: "[N]",
  },
  {
    cmd: "update",
    group: "advanced",
    summary: "show current vs latest version + the shell command to upgrade",
  },
  { cmd: "exit", group: "advanced", summary: "quit the TUI", aliases: ["quit", "q"] },
];

export function suggestSlashCommands(
  prefix: string,
  codeMode = false,
  counts?: Readonly<Record<string, number>>,
): SlashCommandSpec[] {
  const p = prefix.toLowerCase();
  const matches = SLASH_COMMANDS.filter((c) => {
    if (c.contextual === "code" && !codeMode) return false;
    // Empty prefix = browsing the menu — advanced stays hidden until a letter is typed.
    if (p === "" && c.group === "advanced") return false;
    if (c.cmd.startsWith(p)) return true;
    return c.aliases?.some((a) => a.startsWith(p)) ?? false;
  });
  if (!counts) return matches;
  const indexOf = new Map(matches.map((s, i) => [s.cmd, i]));
  return [...matches].sort((a, b) => {
    const diff = (counts[b.cmd] ?? 0) - (counts[a.cmd] ?? 0);
    if (diff !== 0) return diff;
    return (indexOf.get(a.cmd) ?? 0) - (indexOf.get(b.cmd) ?? 0);
  });
}

export function countAdvancedCommands(codeMode: boolean): number {
  return SLASH_COMMANDS.filter(
    (c) => c.group === "advanced" && (c.contextual !== "code" || codeMode),
  ).length;
}

/** alias → canonical cmd map, derived from SLASH_COMMANDS at module init. */
const ALIAS_TO_CMD: Readonly<Record<string, string>> = (() => {
  const m: Record<string, string> = {};
  for (const spec of SLASH_COMMANDS) {
    if (!spec.aliases) continue;
    for (const a of spec.aliases) m[a] = spec.cmd;
  }
  return m;
})();

export function resolveSlashAlias(name: string): string {
  return ALIAS_TO_CMD[name] ?? name;
}

/** Picker fires only when arg tail has no internal whitespace; past that it's a usage hint. */
export function detectSlashArgContext(input: string, codeMode = false): SlashArgContext | null {
  const m = /^\/(\S+) ([\s\S]*)$/.exec(input);
  if (!m) return null;
  const cmdName = resolveSlashAlias(m[1]!.toLowerCase());
  const tail = m[2] ?? "";
  const spec = SLASH_COMMANDS.find(
    (s) => s.cmd === cmdName && (s.contextual !== "code" || codeMode),
  );
  if (!spec) return null;
  const hasInternalSpace = /\s/.test(tail);
  const partialOffset = input.length - tail.length;
  if (hasInternalSpace) {
    return { spec, partial: tail, partialOffset, kind: "hint" };
  }
  return {
    spec,
    partial: tail,
    partialOffset,
    kind: spec.argCompleter ? "picker" : "hint",
  };
}

export function parseSlash(text: string): { cmd: string; args: string[] } | null {
  if (!text.startsWith("/")) return null;
  const parts = text.slice(1).trim().split(/\s+/);
  const cmd = parts[0]?.toLowerCase() ?? "";
  if (!cmd) return null;
  return { cmd, args: parts.slice(1) };
}
