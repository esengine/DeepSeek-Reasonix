// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React, { useEffect, useRef, useState } from "react";
import {
  CharPool,
  type Handle,
  HyperlinkPool,
  StylePool,
  inkCompat,
  mount,
  useKeystroke,
} from "../../renderer/index.js";
import { MarkdownView } from "../ui/markdown-view.js";
import type { Card } from "../ui/state/cards.js";
import type { AgentEvent } from "../ui/state/events.js";
import { AgentStoreProvider, useAgentState, useDispatch } from "../ui/state/provider.js";
import type { SessionInfo } from "../ui/state/state.js";

const BRAND = "#79c0ff";
const FAINT = "#6e7681";
const META = "#8b949e";
const ACCENT = "#d2a8ff";
const OK = "#7ee787";

const SPINNER = ["⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"] as const;
const SPINNER_TICK_MS = 80;

export const DEMO_SESSION: SessionInfo = {
  id: "chat-v2-demo",
  branch: "main",
  workspace: "(demo)",
  model: "deepseek-chat",
};

export interface ScriptStep {
  readonly delayMs: number;
  readonly event: AgentEvent;
}

/** Canned turn lifecycle. user → reasoning → streaming → tool → turn.end. Plays once. */
export function buildScript(): ReadonlyArray<ScriptStep> {
  const reasonId = "r-1";
  const replyId = "s-1";
  const toolId = "t-1";
  const reasonChunks = [
    "The user wants the streaming pipeline preview. ",
    "I'll dispatch a few staged events through the real reducer ",
    "and let the cell-diff renderer paint the cards.",
  ];
  const replyChunks = [
    "## Mount path\n\n",
    "Each card is dispatched through the real reducer and rendered ",
    "**without Ink** — only the changed cells get patched on stdout.\n\n",
    "Key bits:\n\n",
    "- `mount()` from `src/renderer/index.ts:19`\n",
    "- markdown via `markdownToLines()`\n",
    "- spans wrapped in `inkCompat.Text`\n",
  ];
  return [
    { delayMs: 200, event: { type: "user.submit", text: "show me the chat-v2 mount" } },
    { delayMs: 200, event: { type: "turn.start", turnId: "turn-1" } },
    { delayMs: 100, event: { type: "reasoning.start", id: reasonId } },
    { delayMs: 200, event: { type: "reasoning.chunk", id: reasonId, text: reasonChunks[0]! } },
    { delayMs: 200, event: { type: "reasoning.chunk", id: reasonId, text: reasonChunks[1]! } },
    { delayMs: 200, event: { type: "reasoning.chunk", id: reasonId, text: reasonChunks[2]! } },
    {
      delayMs: 100,
      event: { type: "reasoning.end", id: reasonId, paragraphs: 1, tokens: 42 },
    },
    {
      delayMs: 100,
      event: { type: "tool.start", id: toolId, name: "shell", args: { cmd: "ls" } },
    },
    { delayMs: 250, event: { type: "tool.chunk", id: toolId, text: "src/\nrenderer/\n" } },
    {
      delayMs: 250,
      event: {
        type: "tool.end",
        id: toolId,
        output: "src/\nrenderer/\n",
        exitCode: 0,
        elapsedMs: 230,
      },
    },
    { delayMs: 100, event: { type: "streaming.start", id: replyId } },
    { delayMs: 200, event: { type: "streaming.chunk", id: replyId, text: replyChunks[0]! } },
    { delayMs: 200, event: { type: "streaming.chunk", id: replyId, text: replyChunks[1]! } },
    { delayMs: 200, event: { type: "streaming.chunk", id: replyId, text: replyChunks[2]! } },
    { delayMs: 100, event: { type: "streaming.end", id: replyId } },
    {
      delayMs: 200,
      event: {
        type: "turn.end",
        usage: { prompt: 120, reason: 42, output: 36, cacheHit: 0.5, cost: 0.00034 },
        elapsedMs: 1800,
      },
    },
  ];
}

function Header({ inProgress, frame }: { inProgress: boolean; frame: number }): React.ReactElement {
  const glyph = inProgress ? (SPINNER[frame % SPINNER.length] ?? "·") : "◈";
  return (
    <inkCompat.Box flexDirection="row" gap={1}>
      <inkCompat.Text color={inProgress ? BRAND : ACCENT} bold>
        {glyph}
      </inkCompat.Text>
      <inkCompat.Text color={BRAND} bold>
        Reasonix
      </inkCompat.Text>
      <inkCompat.Text color={FAINT}>chat-v2 · cell-diff renderer · Esc to exit</inkCompat.Text>
    </inkCompat.Box>
  );
}

function previewLine(text: string, max = 72): string {
  const flat = text.replace(/\s+/g, " ").trim();
  if (flat.length <= max) return flat;
  return `${flat.slice(0, max - 1)}…`;
}

interface CardHeader {
  readonly glyph: string;
  readonly tone: string;
  readonly head: string;
}

function headerFor(card: Card): CardHeader {
  switch (card.kind) {
    case "user":
      return { glyph: "›", tone: ACCENT, head: "you" };
    case "reasoning":
      return {
        glyph: card.streaming ? "◇" : "◆",
        tone: META,
        head: card.streaming ? "reasoning…" : `reasoning · ${card.tokens}t`,
      };
    case "streaming":
      return {
        glyph: card.done ? "‹" : "▸",
        tone: card.done ? OK : BRAND,
        head: card.done ? "reply" : "streaming…",
      };
    case "tool": {
      const status = card.aborted
        ? "aborted"
        : card.rejected
          ? "rejected"
          : card.done
            ? card.exitCode === 0 || card.exitCode === undefined
              ? "ok"
              : `exit ${card.exitCode}`
            : "running";
      const tone = card.done && !card.aborted && !card.rejected ? OK : BRAND;
      return { glyph: card.done ? "▣" : "▢", tone, head: `${card.name} · ${status}` };
    }
    case "live":
      return { glyph: "·", tone: META, head: card.variant };
    default:
      return { glyph: "·", tone: META, head: card.kind };
  }
}

function CardBody({ card }: { card: Card }): React.ReactElement | null {
  switch (card.kind) {
    case "user":
      return <inkCompat.Text>{previewLine(card.text)}</inkCompat.Text>;
    case "reasoning":
    case "streaming":
      return card.text.length > 0 ? <MarkdownView text={card.text} /> : null;
    case "tool":
      return (
        <inkCompat.Text color={FAINT}>{previewLine(card.output) || "(no output)"}</inkCompat.Text>
      );
    case "live":
      return <inkCompat.Text>{previewLine(card.text)}</inkCompat.Text>;
    default:
      return null;
  }
}

function CardRow({ card }: { card: Card }): React.ReactElement {
  const { glyph, tone, head } = headerFor(card);
  const body = <CardBody card={card} />;
  return (
    <inkCompat.Box flexDirection="column">
      <inkCompat.Box flexDirection="row" gap={1}>
        <inkCompat.Text color={tone} bold>
          {glyph}
        </inkCompat.Text>
        <inkCompat.Text color={tone}>{head}</inkCompat.Text>
      </inkCompat.Box>
      {body ? (
        <inkCompat.Box flexDirection="column" paddingLeft={2}>
          {body}
        </inkCompat.Box>
      ) : null}
    </inkCompat.Box>
  );
}

function TurnTrailer(): React.ReactElement | null {
  const status = useAgentState((s) => s.status);
  if (status.cost === 0 && status.sessionCost === 0) return null;
  return (
    <inkCompat.Box flexDirection="row" gap={2} marginTop={1}>
      <inkCompat.Text color={FAINT}>
        {`turn $${status.cost.toFixed(5)} · session $${status.sessionCost.toFixed(5)} · cache ${(status.cacheHit * 100).toFixed(0)}%`}
      </inkCompat.Text>
    </inkCompat.Box>
  );
}

interface ShellProps {
  readonly script: ReadonlyArray<ScriptStep>;
  readonly onExit: () => void;
}

export function ChatV2Shell({ script, onExit }: ShellProps): React.ReactElement {
  const cards = useAgentState((s) => s.cards);
  const inProgress = useAgentState((s) => s.turnInProgress);
  const dispatch = useDispatch();
  const [frame, setFrame] = useState(0);
  const [done, setDone] = useState(false);
  const playedRef = useRef(false);

  useKeystroke((k) => {
    if (k.escape || (k.ctrl && k.input === "c")) onExit();
  });

  useEffect(() => {
    if (playedRef.current) return;
    playedRef.current = true;
    let cancelled = false;
    let timer: ReturnType<typeof setTimeout> | null = null;
    let i = 0;
    const step = () => {
      if (cancelled || i >= script.length) {
        if (!cancelled) setDone(true);
        return;
      }
      const cur = script[i]!;
      timer = setTimeout(() => {
        if (cancelled) return;
        dispatch(cur.event);
        i++;
        step();
      }, cur.delayMs);
    };
    step();
    return () => {
      cancelled = true;
      if (timer) clearTimeout(timer);
    };
  }, [script, dispatch]);

  useEffect(() => {
    if (!inProgress) return;
    const id = setInterval(() => setFrame((f) => f + 1), SPINNER_TICK_MS);
    return () => clearInterval(id);
  }, [inProgress]);

  return (
    <inkCompat.Box flexDirection="column">
      <Header inProgress={inProgress} frame={frame} />
      <inkCompat.Box flexDirection="column" marginTop={1} gap={1}>
        {cards.map((c) => (
          <CardRow key={c.id} card={c} />
        ))}
      </inkCompat.Box>
      <TurnTrailer />
      {done ? (
        <inkCompat.Box marginTop={1}>
          <inkCompat.Text color={FAINT}>— end of demo · press Esc to exit —</inkCompat.Text>
        </inkCompat.Box>
      ) : null}
    </inkCompat.Box>
  );
}

export interface ChatV2Options {
  readonly stdout?: NodeJS.WriteStream;
  readonly stdin?: NodeJS.ReadStream;
  /** Override the canned playback (used by tests). */
  readonly script?: ReadonlyArray<ScriptStep>;
}

export async function runChatV2(opts: ChatV2Options = {}): Promise<void> {
  const stdout = opts.stdout ?? process.stdout;
  const stdin = opts.stdin ?? process.stdin;

  if (!stdin.isTTY || !stdout.isTTY) {
    process.stderr.write("chat-v2 requires an interactive TTY.\n");
    process.exit(1);
  }

  const pools = {
    char: new CharPool(),
    style: new StylePool(),
    hyperlink: new HyperlinkPool(),
  };

  let resolveExit: () => void = () => {};
  const exited = new Promise<void>((resolve) => {
    resolveExit = resolve;
  });

  const script = opts.script ?? buildScript();

  const handle: Handle = mount(
    <AgentStoreProvider session={DEMO_SESSION}>
      <ChatV2Shell script={script} onExit={() => resolveExit()} />
    </AgentStoreProvider>,
    {
      viewportWidth: stdout.columns ?? 80,
      viewportHeight: stdout.rows ?? 24,
      pools,
      write: (bytes) => stdout.write(bytes),
      stdin,
      onExit: () => resolveExit(),
    },
  );

  const onResize = () => handle.resize(stdout.columns ?? 80, stdout.rows ?? 24);
  stdout.on("resize", onResize);

  try {
    await exited;
  } finally {
    stdout.off("resize", onResize);
    handle.destroy();
    stdin.pause();
  }
}
