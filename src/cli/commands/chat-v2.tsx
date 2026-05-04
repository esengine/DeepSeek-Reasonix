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
import { isSlashInput, runSlash } from "../ui/chat-v2-slash.js";
import { MarkdownView } from "../ui/markdown-view.js";
import { SimplePromptInput } from "../ui/prompt-input-v2.js";
import type { Card } from "../ui/state/cards.js";
import type { AgentEvent } from "../ui/state/events.js";
import {
  AgentStoreProvider,
  useAgentState,
  useAgentStore,
  useDispatch,
} from "../ui/state/provider.js";
import type { SessionInfo } from "../ui/state/state.js";
import { usePromptHistory } from "../ui/use-prompt-history.js";

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
  const session = useAgentState((s) => s.session);
  const showSession = session.id !== DEMO_SESSION.id;
  return (
    <inkCompat.Box flexDirection="row" gap={1}>
      <inkCompat.Text color={inProgress ? BRAND : ACCENT} bold>
        {glyph}
      </inkCompat.Text>
      <inkCompat.Text color={BRAND} bold>
        Reasonix
      </inkCompat.Text>
      <inkCompat.Text color={FAINT}>
        {showSession
          ? `chat-v2 · session "${session.id}" · Esc on empty to exit`
          : "chat-v2 · cell-diff renderer · Esc on empty to exit"}
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

/** Run a single user-turn. The default implementation plays the canned
 *  buildReply script; runChatV2 swaps in a real DeepSeek loop when an API
 *  key is available. */
export type RunTurn = (
  userText: string,
  turn: number,
  dispatch: (ev: AgentEvent) => void,
) => Promise<void>;

interface ShellProps {
  readonly onExit: () => void;
  /** Override how a turn plays out — tests pass a zero-delay canned script,
   *  the real entry passes a loop-driven streamer. Default: canned demo. */
  readonly runTurn?: RunTurn;
}

export function makeCannedRunTurn(
  builder: (userText: string, turn: number) => ReadonlyArray<ScriptStep>,
): RunTurn {
  return (userText, turn, dispatch) =>
    new Promise((resolve) => {
      const steps = builder(userText, turn);
      let i = 0;
      const step = (): void => {
        if (i >= steps.length) {
          resolve();
          return;
        }
        const cur = steps[i]!;
        setTimeout(() => {
          dispatch(cur.event);
          i++;
          step();
        }, cur.delayMs);
      };
      step();
    });
}

const defaultRunTurn: RunTurn = makeCannedRunTurn(buildReply);

export function ChatV2Shell({ onExit, runTurn = defaultRunTurn }: ShellProps): React.ReactElement {
  const cards = useAgentState((s) => s.cards);
  const inProgress = useAgentState((s) => s.turnInProgress);
  const dispatch = useDispatch();
  const store = useAgentStore();
  const [frame, setFrame] = useState(0);
  const [draft, setDraft] = useState("");
  const turnRef = useRef(0);
  const dispatchRef = useRef(dispatch);
  dispatchRef.current = dispatch;
  const runTurnRef = useRef(runTurn);
  runTurnRef.current = runTurn;
  const onExitRef = useRef(onExit);
  onExitRef.current = onExit;
  const history = usePromptHistory();

  useEffect(() => {
    if (!inProgress) return;
    const id = setInterval(() => setFrame((f) => f + 1), SPINNER_TICK_MS);
    return () => clearInterval(id);
  }, [inProgress]);

  const handleSubmit = (text: string): void => {
    const trimmed = text.trim();
    if (trimmed.length === 0) return;
    if (inProgress) return;
    history.recordSubmit(trimmed);
    setDraft("");

    if (isSlashInput(trimmed)) {
      const outcome = runSlash(trimmed, { state: store.getState() });
      for (const ev of outcome.events) dispatchRef.current(ev);
      if (outcome.exit) onExitRef.current();
      return;
    }

    const turn = ++turnRef.current;
    dispatchRef.current({ type: "user.submit", text: trimmed });
    void runTurnRef.current(trimmed, turn, (ev) => dispatchRef.current(ev));
  };

  const handleHistoryPrev = (): void => {
    const recalled = history.recallPrev(draft);
    if (recalled !== null) setDraft(recalled);
  };

  const handleHistoryNext = (): void => {
    const recalled = history.recallNext();
    if (recalled !== null) setDraft(recalled);
  };

  // Split cards into settled vs in-flight. Settled cards flow through
  // inkCompat.Static — once promoted they live in terminal scrollback and
  // never re-render, so a long stream of replies can't blow past the
  // viewport's live region. Only the actively-streaming card stays live.
  const cutoff = firstUnsettledIndex(cards);
  const settled = cards.slice(0, cutoff);
  const live = cards.slice(cutoff);

  return (
    <inkCompat.Box flexDirection="column">
      <Header inProgress={inProgress} frame={frame} />
      <inkCompat.Box flexDirection="column" marginTop={1} gap={1}>
        <inkCompat.Static items={settled}>
          {(card) => <CardRow key={card.id} card={card} />}
        </inkCompat.Static>
        {live.map((c) => (
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
          onHistoryPrev={handleHistoryPrev}
          onHistoryNext={handleHistoryNext}
          disabled={inProgress}
          placeholder={inProgress ? "thinking…" : "type a message and hit enter…"}
        />
      </inkCompat.Box>
    </inkCompat.Box>
  );
}

/** First index whose card is still in-flight. Anything before it is safe to
 *  promote to scrollback because no future event can mutate it. */
export function firstUnsettledIndex(cards: ReadonlyArray<Card>): number {
  for (let i = 0; i < cards.length; i++) {
    if (!isSettled(cards[i] as Card)) return i;
  }
  return cards.length;
}

function isSettled(card: Card): boolean {
  switch (card.kind) {
    case "streaming":
    case "tool":
    case "branch":
      return card.done;
    case "reasoning":
      return !card.streaming;
    case "plan":
      if (card.variant !== "active") return true;
      return card.steps.every((s) => s.status === "done" || s.status === "skipped");
    default:
      return true;
  }
}

export interface ChatV2Options {
  readonly stdout?: NodeJS.WriteStream;
  readonly stdin?: NodeJS.ReadStream;
  readonly model?: string;
  readonly system?: string;
  /** Session name to persist to / resume from. Default: ad-hoc unnamed. */
  readonly session?: string;
  /** With `session`: force a fresh timestamped suffix instead of resuming. */
  readonly forceNew?: boolean;
  /** With `session`: pick the latest `name-*` prefix even if `name` itself exists. */
  readonly forceResume?: boolean;
}

const DEFAULT_SYSTEM =
  "You are Reasonix, a helpful DeepSeek-powered assistant. Be concise and accurate.";

async function loadInitialCards(sessionName: string): Promise<ReadonlyArray<Card> | undefined> {
  const { loadSessionMessages } = await import("../../memory/session.js");
  const msgs = loadSessionMessages(sessionName);
  if (msgs.length === 0) return undefined;
  // Replay only the user-side turns as cards. Replaying assistant content
  // would require a full re-render of streaming + tool sequences, which
  // we don't have on disk in event form. The user's own messages are
  // enough context for the recall — the new turn's response will arrive
  // streamed from the model.
  const cards: Card[] = [];
  let seq = 0;
  for (const m of msgs) {
    if (m.role !== "user") continue;
    const text = typeof m.content === "string" ? m.content : "";
    if (text.length === 0) continue;
    cards.push({
      kind: "user",
      id: `replay-user-${seq++}`,
      ts: Date.now(),
      text,
    });
  }
  return cards.length > 0 ? cards : undefined;
}

async function buildDefaultTools(): Promise<import("../../tools.js").ToolRegistry> {
  const { ToolRegistry } = await import("../../tools.js");
  const { searchEnabled } = await import("../../config.js");
  const { registerWebTools } = await import("../../tools/web.js");
  const { registerMemoryTools } = await import("../../tools/memory.js");
  const { registerChoiceTool } = await import("../../tools/choice.js");
  const tools = new ToolRegistry();
  if (searchEnabled()) registerWebTools(tools);
  registerMemoryTools(tools, {});
  registerChoiceTool(tools);
  return tools;
}

function makeRealRunTurn(model: string, system: string, session: string | undefined): RunTurn {
  // The loop construction is per-turn so dependencies stay lazy; the same
  // CacheFirstLoop instance is reused via closure to preserve cache state
  // and the running session transcript across turns.
  let loopPromise: Promise<{
    loop: import("../../loop.js").CacheFirstLoop;
  }> | null = null;

  const setupLoop = async () => {
    const { DeepSeekClient, ImmutablePrefix, CacheFirstLoop } = await import("../../index.js");
    const tools = await buildDefaultTools();
    const client = new DeepSeekClient();
    const prefix = new ImmutablePrefix({ system, toolSpecs: tools.specs() });
    const loop = new CacheFirstLoop({ client, prefix, model, tools, session });
    return { loop };
  };

  return async (userText, turn, dispatch) => {
    const { makeLoopBridge } = await import("../ui/loop-bridge.js");
    const bridge = makeLoopBridge(`turn-${turn}`);
    if (!loopPromise) loopPromise = setupLoop();
    const { loop } = await loopPromise;
    try {
      for await (const ev of loop.step(userText)) {
        for (const out of bridge.consume(ev)) dispatch(out);
      }
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      for (const out of bridge.consume({ turn, role: "error", content: msg, error: msg })) {
        dispatch(out);
      }
    }
  };
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

  // Enable bracketed-paste so the keystroke parser sees pastes as one
  // atomic chunk and SimplePromptInput can sentinel-fold them.
  stdout.write("\x1b[?2004h");

  // Real DeepSeek loop when an API key is present; canned demo otherwise.
  const apiKey = process.env.DEEPSEEK_API_KEY;
  const { resolved: resolvedSession } = apiKey
    ? (await import("../../memory/session.js")).resolveSession(
        opts.session,
        opts.forceNew,
        opts.forceResume,
      )
    : { resolved: undefined };
  const runTurn: RunTurn | undefined = apiKey
    ? makeRealRunTurn(opts.model ?? "deepseek-chat", opts.system ?? DEFAULT_SYSTEM, resolvedSession)
    : undefined;

  const sessionInfo: SessionInfo = resolvedSession
    ? { ...DEMO_SESSION, id: resolvedSession }
    : DEMO_SESSION;

  // Replay user-side history from disk so resumed sessions show prior turns.
  const initialCards = resolvedSession ? await loadInitialCards(resolvedSession) : undefined;

  const handle: Handle = mount(
    <AgentStoreProvider session={sessionInfo} initialCards={initialCards}>
      <ChatV2Shell onExit={() => resolveExit()} runTurn={runTurn} />
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
    stdout.write("\x1b[?2004l");
    stdin.pause();
  }
}
