/** Slash command handlers for chat-v2 — synthesise AgentEvents instead of running a turn. */

import type { AgentEvent } from "./state/events.js";
import type { AgentState } from "./state/state.js";

export interface SlashContext {
  readonly state: AgentState;
}

export interface SlashOutcome {
  /** Events to dispatch in order. */
  readonly events: ReadonlyArray<AgentEvent>;
  /** When true, run the parent's `onExit` after dispatching. */
  readonly exit?: boolean;
  /** When true, do NOT run a model turn after this command. */
  readonly handled: true;
}

export interface SlashCommand {
  readonly name: string;
  readonly summary: string;
  readonly run: (raw: string, ctx: SlashContext) => SlashOutcome;
}

const HELP_TEXT = `## Available commands

- \`/help\` — show this list
- \`/clear\` — wipe the conversation cards
- \`/cost\` — show session usage and cost
- \`/new\` — clear and start a fresh turn count
- \`/exit\` — leave chat-v2

Tip: **Alt+Enter** inserts a newline; **↑/↓** walks history.
`;

const HELP: SlashCommand = {
  name: "/help",
  summary: "list available commands",
  run: () => ({
    events: [synthUser("/help"), ...synthReply("help", HELP_TEXT)],
    handled: true,
  }),
};

const CLEAR: SlashCommand = {
  name: "/clear",
  summary: "wipe the conversation",
  run: () => ({
    events: [{ type: "session.reset" }],
    handled: true,
  }),
};

const COST: SlashCommand = {
  name: "/cost",
  summary: "show session usage and cost",
  run: (_raw, { state }) => {
    const { cost, sessionCost, cacheHit } = state.status;
    const body = [
      "## Session cost",
      "",
      `- last turn: \`$${cost.toFixed(5)}\``,
      `- session total: \`$${sessionCost.toFixed(5)}\``,
      `- cache hit: \`${(cacheHit * 100).toFixed(0)}%\``,
      "",
    ].join("\n");
    return {
      events: [synthUser("/cost"), ...synthReply("cost", body)],
      handled: true,
    };
  },
};

const NEW: SlashCommand = {
  name: "/new",
  summary: "clear cards and start a fresh turn",
  run: () => ({
    events: [{ type: "session.reset" }],
    handled: true,
  }),
};

const EXIT: SlashCommand = {
  name: "/exit",
  summary: "leave chat-v2",
  run: () => ({
    events: [],
    exit: true,
    handled: true,
  }),
};

export const SLASH_COMMANDS: ReadonlyArray<SlashCommand> = [HELP, CLEAR, COST, NEW, EXIT];

const COMMAND_MAP: ReadonlyMap<string, SlashCommand> = new Map(
  SLASH_COMMANDS.map((c) => [c.name, c]),
);

export function isSlashInput(text: string): boolean {
  return text.trimStart().startsWith("/");
}

export function runSlash(raw: string, ctx: SlashContext): SlashOutcome {
  const trimmed = raw.trim();
  const head = trimmed.split(/\s+/, 1)[0] ?? trimmed;
  const cmd = COMMAND_MAP.get(head);
  if (cmd) return cmd.run(trimmed, ctx);
  return {
    events: [
      synthUser(trimmed),
      {
        type: "live.show",
        id: `slash-err-${Date.now()}`,
        ts: Date.now(),
        variant: "sessionOp",
        tone: "err",
        text: `unknown command: ${head}. type /help for the list.`,
      },
    ],
    handled: true,
  };
}

function synthUser(text: string): AgentEvent {
  return { type: "user.submit", text };
}

function synthReply(tag: string, text: string): ReadonlyArray<AgentEvent> {
  const id = `slash-${tag}-${Date.now()}`;
  const turnId = `slash-turn-${Date.now()}`;
  return [
    { type: "turn.start", turnId },
    { type: "streaming.start", id },
    { type: "streaming.chunk", id, text },
    { type: "streaming.end", id },
    {
      type: "turn.end",
      usage: { prompt: 0, reason: 0, output: 0, cacheHit: 0, cost: 0 },
    },
  ];
}
