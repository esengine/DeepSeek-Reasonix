// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React, { useEffect, useRef, useState } from "react";
import {
  CharPool,
  type Handle,
  HyperlinkPool,
  StylePool,
  inkCompat,
  mount,
} from "../../renderer/index.js";
import { MarkdownView } from "../ui/markdown-view.js";
import { SimplePromptInput } from "../ui/prompt-input-v2.js";
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

/** Builds a canned reply turn for the given user text. The chat-v2 demo
 *  isn't backed by a real model, so each submission gets a stock
 *  reasoning + streaming cycle that quotes the input back. */
export function buildReply(userText: string, turn: number): ReadonlyArray<ScriptStep> {
  const reasonId = `r-${turn}`;
  const replyId = `s-${turn}`;
  return [
    { delayMs: 50, event: { type: "turn.start", turnId: `t-${turn}` } },
    { delayMs: 50, event: { type: "reasoning.start", id: reasonId } },
    {
      delayMs: 80,
      event: {
        type: "reasoning.chunk",
        id: reasonId,
        text: "Routing the message through the cell-diff renderer demo. ",
      },
    },
    {
      delayMs: 80,
      event: {
        type: "reasoning.chunk",
        id: reasonId,
        text: "No real model is wired up — replies are canned.",
      },
    },
    {
      delayMs: 60,
      event: { type: "reasoning.end", id: reasonId, paragraphs: 1, tokens: 18 },
    },
    { delayMs: 50, event: { type: "streaming.start", id: replyId } },
    {
      delayMs: 80,
      event: {
        type: "streaming.chunk",
        id: replyId,
        text: `## You said\n\n> ${userText}\n\n`,
      },
    },
    {
      delayMs: 80,
      event: {
        type: "streaming.chunk",
        id: replyId,
        text: "Each card flows through the real reducer at ",
      },
    },
    {
      delayMs: 80,
      event: {
        type: "streaming.chunk",
        id: replyId,
        text: "`src/cli/ui/state/reducer.ts`, then renders via `inkCompat`.\n",
      },
    },
    { delayMs: 50, event: { type: "streaming.end", id: replyId } },
    {
      delayMs: 80,
      event: {
        type: "turn.end",
        usage: { prompt: 80, reason: 18, output: 28, cacheHit: 0.5, cost: 0.00021 },
        elapsedMs: 600,
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
      <inkCompat.Text color={FAINT}>
        chat-v2 · cell-diff renderer · type a message · Esc on empty to exit
      </inkCompat.Text>
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
  readonly onExit: () => void;
  /** Override the reply-script generator. Tests use a zero-delay variant. */
  readonly buildReply?: (userText: string, turn: number) => ReadonlyArray<ScriptStep>;
}

export function ChatV2Shell({
  onExit,
  buildReply: buildReplyOverride,
}: ShellProps): React.ReactElement {
  const cards = useAgentState((s) => s.cards);
  const inProgress = useAgentState((s) => s.turnInProgress);
  const dispatch = useDispatch();
  const [frame, setFrame] = useState(0);
  const [draft, setDraft] = useState("");
  const turnRef = useRef(0);
  const dispatchRef = useRef(dispatch);
  dispatchRef.current = dispatch;

  const replyBuilder = buildReplyOverride ?? buildReply;

  useEffect(() => {
    if (!inProgress) return;
    const id = setInterval(() => setFrame((f) => f + 1), SPINNER_TICK_MS);
    return () => clearInterval(id);
  }, [inProgress]);

  const playReply = (text: string): void => {
    const turn = ++turnRef.current;
    const steps = replyBuilder(text, turn);
    let i = 0;
    const step = (): void => {
      if (i >= steps.length) return;
      const cur = steps[i]!;
      setTimeout(() => {
        dispatchRef.current(cur.event);
        i++;
        step();
      }, cur.delayMs);
    };
    step();
  };

  const handleSubmit = (text: string): void => {
    const trimmed = text.trim();
    if (trimmed.length === 0) return;
    if (inProgress) return;
    dispatchRef.current({ type: "user.submit", text: trimmed });
    setDraft("");
    playReply(trimmed);
  };

  return (
    <inkCompat.Box flexDirection="column">
      <Header inProgress={inProgress} frame={frame} />
      <inkCompat.Box flexDirection="column" marginTop={1} gap={1}>
        {cards.map((c) => (
          <CardRow key={c.id} card={c} />
        ))}
      </inkCompat.Box>
      <TurnTrailer />
      <inkCompat.Box marginTop={1}>
        <SimplePromptInput
          value={draft}
          onChange={setDraft}
          onSubmit={handleSubmit}
          onCancel={onExit}
          disabled={inProgress}
          placeholder={inProgress ? "thinking…" : "type a message and hit enter…"}
        />
      </inkCompat.Box>
    </inkCompat.Box>
  );
}

export interface ChatV2Options {
  readonly stdout?: NodeJS.WriteStream;
  readonly stdin?: NodeJS.ReadStream;
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

  const handle: Handle = mount(
    <AgentStoreProvider session={DEMO_SESSION}>
      <ChatV2Shell onExit={() => resolveExit()} />
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
