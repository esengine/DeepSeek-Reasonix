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

const BRAND = "#79c0ff";
const FAINT = "#6e7681";
const META = "#8b949e";
const ACCENT = "#d2a8ff";
const OK = "#7ee787";
const WARN = "#f0b07d";
const ERR = "#ff8b81";
const PEND = "#484f58";
const SPINNER = ["⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"] as const;

interface UserItem {
  readonly kind: "user";
  readonly text: string;
}
interface ReasoningSettled {
  readonly kind: "reasoning";
  readonly lines: number;
  readonly seconds: number;
}
interface ToolSettled {
  readonly kind: "tool";
  readonly name: string;
  readonly summary: string;
  readonly seconds: number;
  readonly ok: boolean;
}
interface PlanSettled {
  readonly kind: "plan";
  readonly steps: number;
  readonly seconds: number;
}
interface ResponseSettled {
  readonly kind: "response";
  readonly text: string;
}
interface DiffItem {
  readonly kind: "diff";
  readonly file: string;
  readonly added: number;
  readonly removed: number;
}
interface ErrorItem {
  readonly kind: "error";
  readonly message: string;
}

type StaticItem =
  | UserItem
  | ReasoningSettled
  | ToolSettled
  | PlanSettled
  | ResponseSettled
  | DiffItem
  | ErrorItem;

function StaticRow({ item }: { item: StaticItem }): React.ReactElement {
  switch (item.kind) {
    case "user":
      return (
        <inkCompat.Box flexDirection="row" gap={1}>
          <inkCompat.Text color={ACCENT}>›</inkCompat.Text>
          <inkCompat.Text>{item.text}</inkCompat.Text>
        </inkCompat.Box>
      );
    case "reasoning":
      return (
        <inkCompat.Box flexDirection="row" gap={1}>
          <inkCompat.Text color={OK}>✓</inkCompat.Text>
          <inkCompat.Text color={META}>
            {`reasoned · ${item.lines} lines · ${item.seconds.toFixed(1)}s`}
          </inkCompat.Text>
        </inkCompat.Box>
      );
    case "tool":
      return (
        <inkCompat.Box flexDirection="row" gap={1}>
          <inkCompat.Text color={item.ok ? OK : ERR}>{item.ok ? "✓" : "✖"}</inkCompat.Text>
          <inkCompat.Text color={META}>
            {`${item.name} · ${item.summary} · ${item.seconds.toFixed(1)}s`}
          </inkCompat.Text>
        </inkCompat.Box>
      );
    case "plan":
      return (
        <inkCompat.Box flexDirection="row" gap={1}>
          <inkCompat.Text color={OK}>✓</inkCompat.Text>
          <inkCompat.Text color={META}>
            {`Plan · ${item.steps} steps · ${item.seconds.toFixed(1)}s`}
          </inkCompat.Text>
        </inkCompat.Box>
      );
    case "response":
      return (
        <inkCompat.Box flexDirection="row" gap={1}>
          <inkCompat.Text color={OK}>‹</inkCompat.Text>
          <inkCompat.Text>{item.text}</inkCompat.Text>
        </inkCompat.Box>
      );
    case "diff":
      return (
        <inkCompat.Box flexDirection="row" gap={1}>
          <inkCompat.Text color={WARN}>±</inkCompat.Text>
          <inkCompat.Text>{item.file}</inkCompat.Text>
          <inkCompat.Text color={OK}>{`+${item.added}`}</inkCompat.Text>
          <inkCompat.Text color={ERR}>{`-${item.removed}`}</inkCompat.Text>
        </inkCompat.Box>
      );
    case "error":
      return (
        <inkCompat.Box flexDirection="row" gap={1}>
          <inkCompat.Text color={ERR}>✖</inkCompat.Text>
          <inkCompat.Text color={ERR}>{item.message}</inkCompat.Text>
        </inkCompat.Box>
      );
  }
}

interface ReasoningActive {
  readonly kind: "reasoning";
  readonly text: string;
  readonly frame: number;
}
interface ToolActive {
  readonly kind: "tool";
  readonly name: string;
  readonly outputLines: ReadonlyArray<string>;
  readonly frame: number;
  readonly elapsedMs: number;
}
interface PlanActive {
  readonly kind: "plan";
  readonly steps: ReadonlyArray<{ label: string; status: "done" | "running" | "pending" }>;
  readonly frame: number;
}
interface ResponseActive {
  readonly kind: "response";
  readonly revealed: number;
  readonly fullText: string;
  readonly frame: number;
}
type ActiveCard = ReasoningActive | ToolActive | PlanActive | ResponseActive;

const SHELL_OUTPUT = [
  " PASS  src/loop.test.ts",
  " PASS  src/parser.test.ts",
  " PASS  src/cli/index.test.ts",
  " PASS  src/cli/commands/chat.test.ts",
  " PASS  src/diff/cell.test.ts",
  " PASS  src/diff/screen.test.ts",
  " PASS  src/renderer/layout.test.ts",
  " PASS  src/renderer/diff.test.ts",
  " PASS  src/renderer/serialize.test.ts",
  "",
  "Tests: 142 passed",
] as const;
const SHELL_WINDOW = 5;

const REASONING_TEXT = "Looking at recent failures, the loop test changed.";
const RESPONSE_TEXT =
  "The failing test is on src/loop.test.ts line 42 — the assertion expects the parser to drop the trailing tool-call marker, but the new tokenizer keeps it. Two paths forward.";

function ReasoningCard({ card }: { card: ReasoningActive }): React.ReactElement {
  const spin = SPINNER[card.frame % SPINNER.length] ?? "·";
  return (
    <inkCompat.Box
      flexDirection="column"
      borderStyle="round"
      borderColor={ACCENT}
      paddingX={1}
      marginTop={1}
    >
      <inkCompat.Box flexDirection="row" gap={1}>
        <inkCompat.Text color={BRAND}>{spin}</inkCompat.Text>
        <inkCompat.Text color={META}>thinking…</inkCompat.Text>
      </inkCompat.Box>
      <inkCompat.Text dimColor>{card.text}</inkCompat.Text>
    </inkCompat.Box>
  );
}

function ToolCard({ card }: { card: ToolActive }): React.ReactElement {
  const spin = SPINNER[card.frame % SPINNER.length] ?? "·";
  const total = card.outputLines.length;
  const startIdx = Math.max(0, total - SHELL_WINDOW);
  const visible = card.outputLines.slice(startIdx);
  const hidden = startIdx;
  const seconds = (card.elapsedMs / 1000).toFixed(1);
  return (
    <inkCompat.Box
      flexDirection="column"
      borderStyle="round"
      borderColor={BRAND}
      paddingX={1}
      marginTop={1}
    >
      <inkCompat.Box flexDirection="row" gap={1}>
        <inkCompat.Text color={BRAND}>{spin}</inkCompat.Text>
        <inkCompat.Text color={BRAND} bold>
          {card.name}
        </inkCompat.Text>
        {hidden > 0 ? (
          <inkCompat.Text color={FAINT}>{`(+${hidden} earlier)`}</inkCompat.Text>
        ) : null}
        <inkCompat.Text color={FAINT}>{`${seconds}s`}</inkCompat.Text>
      </inkCompat.Box>
      {visible.map((line, i) => (
        // biome-ignore lint/suspicious/noArrayIndexKey: rolling-window output, positional & append-only
        <inkCompat.Text key={`out-${startIdx + i}`} dimColor>
          {line || " "}
        </inkCompat.Text>
      ))}
    </inkCompat.Box>
  );
}

function PlanCard({ card }: { card: PlanActive }): React.ReactElement {
  const spin = SPINNER[card.frame % SPINNER.length] ?? "·";
  return (
    <inkCompat.Box
      flexDirection="column"
      borderStyle="round"
      borderColor={ACCENT}
      paddingX={1}
      marginTop={1}
    >
      <inkCompat.Text color={ACCENT} bold>
        ⊞ Plan
      </inkCompat.Text>
      {card.steps.map((step) => {
        const glyph = step.status === "done" ? "✓" : step.status === "running" ? spin : "○";
        const color = step.status === "done" ? OK : step.status === "running" ? BRAND : PEND;
        return (
          <inkCompat.Box key={step.label} flexDirection="row" gap={1}>
            <inkCompat.Text color={color}>{glyph}</inkCompat.Text>
            <inkCompat.Text dimColor={step.status === "pending"}>{step.label}</inkCompat.Text>
          </inkCompat.Box>
        );
      })}
    </inkCompat.Box>
  );
}

function ResponseCard({ card }: { card: ResponseActive }): React.ReactElement {
  const spin = SPINNER[card.frame % SPINNER.length] ?? "·";
  const text = card.fullText.slice(0, card.revealed);
  return (
    <inkCompat.Box
      flexDirection="column"
      borderStyle="round"
      borderColor={OK}
      paddingX={1}
      marginTop={1}
    >
      <inkCompat.Box flexDirection="row" gap={1}>
        <inkCompat.Text color={BRAND}>{spin}</inkCompat.Text>
        <inkCompat.Text>{text || "thinking…"}</inkCompat.Text>
      </inkCompat.Box>
    </inkCompat.Box>
  );
}

function StatusRow({ elapsedMs }: { elapsedMs: number }): React.ReactElement {
  const seconds = (elapsedMs / 1000).toFixed(1);
  const cost = (elapsedMs / 1000) * 0.0008;
  return (
    <inkCompat.Box flexDirection="row" gap={2}>
      <inkCompat.Text color={BRAND} bold>
        ◈ Reasonix
      </inkCompat.Text>
      <inkCompat.Text color={META}>deepseek-v3</inkCompat.Text>
      <inkCompat.Text color={FAINT}>{`${seconds}s`}</inkCompat.Text>
      <inkCompat.Text color={FAINT}>{`$${cost.toFixed(4)}`}</inkCompat.Text>
    </inkCompat.Box>
  );
}

function PromptInput(): React.ReactElement {
  return (
    <inkCompat.Box flexDirection="row" gap={1} marginTop={1}>
      <inkCompat.Text color={BRAND} bold>
        ›
      </inkCompat.Text>
      <inkCompat.Text dimColor>type your question…</inkCompat.Text>
      <inkCompat.Text color={FAINT}>▏</inkCompat.Text>
    </inkCompat.Box>
  );
}

function HintBar(): React.ReactElement {
  return (
    <inkCompat.Box marginTop={1}>
      <inkCompat.Text dimColor>card lifecycle demo · auto-replays · Esc exit</inkCompat.Text>
    </inkCompat.Box>
  );
}

interface ShellProps {
  onExit: () => void;
}

export function CardDemoShell({ onExit }: ShellProps): React.ReactElement {
  const [history, setHistory] = useState<ReadonlyArray<StaticItem>>([]);
  const [active, setActive] = useState<ActiveCard | null>(null);
  const [elapsed, setElapsed] = useState(0);
  const startedRef = useRef(Date.now());

  useKeystroke((k) => {
    if (k.escape) onExit();
  });

  useEffect(() => {
    const id = setInterval(() => {
      setElapsed(Date.now() - startedRef.current);
    }, 100);
    return () => clearInterval(id);
  }, []);

  useEffect(() => {
    let cancelled = false;
    const stages = buildScript();
    let i = 0;

    const runNext = (): void => {
      if (cancelled) return;
      if (i >= stages.length) {
        setActive(null);
        setTimeout(() => {
          if (cancelled) return;
          setHistory([]);
          i = 0;
          runNext();
        }, 1500);
        return;
      }
      const stage = stages[i];
      i++;
      if (!stage) return;
      stage(setActive, (item) => {
        setHistory((prev) => [...prev, item]);
      }).then(() => {
        if (!cancelled) runNext();
      });
    };

    runNext();
    return () => {
      cancelled = true;
    };
  }, []);

  return (
    <inkCompat.Box flexDirection="column">
      <StatusRow elapsedMs={elapsed} />
      <inkCompat.Box flexDirection="column">
        {history.map((item, i) => (
          // biome-ignore lint/suspicious/noArrayIndexKey: append-only history list
          <StaticRow key={`h-${i}`} item={item} />
        ))}
      </inkCompat.Box>
      {active ? (
        active.kind === "reasoning" ? (
          <ReasoningCard card={active} />
        ) : active.kind === "tool" ? (
          <ToolCard card={active} />
        ) : active.kind === "plan" ? (
          <PlanCard card={active} />
        ) : (
          <ResponseCard card={active} />
        )
      ) : (
        <PromptInput />
      )}
      <HintBar />
    </inkCompat.Box>
  );
}

type Stage = (
  setActive: (a: ActiveCard | null) => void,
  push: (item: StaticItem) => void,
) => Promise<void>;

function sleep(ms: number): Promise<void> {
  return new Promise((r) => setTimeout(r, ms));
}

function buildScript(): Stage[] {
  return [
    async (_setActive, push) => {
      push({ kind: "user", text: "what's the failing test in the renderer?" });
      await sleep(400);
    },
    async (setActive, push) => {
      const start = Date.now();
      let frame = 0;
      let revealed = 0;
      const interval = setInterval(() => {
        frame++;
        revealed = Math.min(REASONING_TEXT.length, revealed + 4);
        setActive({ kind: "reasoning", text: REASONING_TEXT.slice(0, revealed), frame });
      }, 80);
      await sleep(2000);
      clearInterval(interval);
      const seconds = (Date.now() - start) / 1000;
      setActive(null);
      push({ kind: "reasoning", lines: 1, seconds });
      await sleep(300);
    },
    async (setActive, push) => {
      const start = Date.now();
      let frame = 0;
      const lines: string[] = [];
      const spinTimer = setInterval(() => {
        frame++;
        setActive({
          kind: "tool",
          name: "npm test",
          outputLines: lines.slice(),
          frame,
          elapsedMs: Date.now() - start,
        });
      }, 80);
      const lineTimer = setInterval(() => {
        if (lines.length < SHELL_OUTPUT.length) {
          lines.push(SHELL_OUTPUT[lines.length] ?? "");
          setActive({
            kind: "tool",
            name: "npm test",
            outputLines: lines.slice(),
            frame,
            elapsedMs: Date.now() - start,
          });
        }
      }, 220);
      await sleep(SHELL_OUTPUT.length * 220 + 400);
      clearInterval(spinTimer);
      clearInterval(lineTimer);
      const seconds = (Date.now() - start) / 1000;
      setActive(null);
      push({ kind: "tool", name: "npm test", summary: "142 passed", seconds, ok: true });
      await sleep(300);
    },
    async (setActive, push) => {
      const labels = [
        "identify the failing test",
        "wire the regression check",
        "rebuild dist",
        "publish patch",
      ];
      const start = Date.now();
      let frame = 0;
      let cur = 0;
      const tick = setInterval(() => {
        frame++;
        const steps = labels.map((label, i) => ({
          label,
          status: (i < cur ? "done" : i === cur ? "running" : "pending") as
            | "done"
            | "running"
            | "pending",
        }));
        setActive({ kind: "plan", steps, frame });
      }, 80);
      while (cur < labels.length) {
        await sleep(700);
        cur++;
      }
      clearInterval(tick);
      const seconds = (Date.now() - start) / 1000;
      setActive(null);
      push({ kind: "plan", steps: labels.length, seconds });
      await sleep(300);
    },
    async (_setActive, push) => {
      push({ kind: "diff", file: "src/parser.ts", added: 12, removed: 3 });
      await sleep(400);
    },
    async (setActive, push) => {
      let frame = 0;
      let revealed = 0;
      const tick = setInterval(() => {
        frame++;
        revealed = Math.min(RESPONSE_TEXT.length, revealed + 5);
        setActive({ kind: "response", revealed, fullText: RESPONSE_TEXT, frame });
      }, 33);
      while (revealed < RESPONSE_TEXT.length) await sleep(50);
      clearInterval(tick);
      await sleep(400);
      setActive(null);
      push({ kind: "response", text: RESPONSE_TEXT });
      await sleep(500);
    },
    async (_setActive, push) => {
      push({ kind: "error", message: "rate-limit hit · retrying in 3s…" });
      await sleep(800);
    },
  ];
}

export interface CardDemoOptions {
  readonly stdout?: NodeJS.WriteStream;
  readonly stdin?: NodeJS.ReadStream;
}

export async function runCardDemo(opts: CardDemoOptions = {}): Promise<void> {
  const stdout = opts.stdout ?? process.stdout;
  const stdin = opts.stdin ?? process.stdin;

  if (!stdin.isTTY || !stdout.isTTY) {
    console.error("card-demo requires an interactive TTY.");
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

  const handle: Handle = mount(<CardDemoShell onExit={() => resolveExit()} />, {
    viewportWidth: stdout.columns ?? 80,
    viewportHeight: stdout.rows ?? 30,
    pools,
    write: (bytes) => stdout.write(bytes),
    stdin,
    onExit: () => resolveExit(),
  });

  const onResize = () => handle.resize(stdout.columns ?? 80, stdout.rows ?? 30);
  stdout.on("resize", onResize);

  try {
    await exited;
  } finally {
    stdout.off("resize", onResize);
    handle.destroy();
    stdin.pause();
  }
}
