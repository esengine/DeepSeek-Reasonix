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
const VIOLET = "#b395f5";
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
  readonly tail: ReadonlyArray<string>;
  readonly paragraphs: number;
  readonly tokens: number;
  readonly seconds: number;
}
interface ToolSettled {
  readonly kind: "tool";
  readonly tone: ToolTone;
  readonly name: string;
  readonly args: string;
  readonly output: ReadonlyArray<string>;
  readonly hidden: number;
  readonly seconds: number;
  readonly status: "ok" | "rejected" | "error" | "retry";
  readonly retryInfo?: string;
}
interface PlanStep {
  readonly status: "todo" | "running" | "done" | "skipped" | "failed" | "blocked";
  readonly label: string;
  readonly note?: string;
}
interface PlanSettled {
  readonly kind: "plan";
  readonly steps: ReadonlyArray<PlanStep>;
  readonly seconds: number;
}
interface ResponseSettled {
  readonly kind: "response";
  readonly text: string;
  readonly aborted?: boolean;
}
interface DiffItem {
  readonly kind: "diff";
  readonly file: string;
  readonly added: number;
  readonly removed: number;
  readonly preview: ReadonlyArray<{ kind: "+" | "-" | " "; text: string }>;
}
interface SubAgentSettled {
  readonly kind: "subagent";
  readonly task: string;
  readonly children: ReadonlyArray<SubChild>;
  readonly seconds: number;
  readonly ok: boolean;
}
interface SubChild {
  readonly kind: "reasoning" | "tool" | "diff" | "error";
  readonly summary: string;
}
interface UsageItem {
  readonly kind: "usage";
  readonly inputTokens: number;
  readonly outputTokens: number;
  readonly totalCost: number;
}
interface ErrorItem {
  readonly kind: "error";
  readonly message: string;
}
interface WarnItem {
  readonly kind: "warn";
  readonly message: string;
}

type StaticItem =
  | UserItem
  | ReasoningSettled
  | ToolSettled
  | PlanSettled
  | ResponseSettled
  | DiffItem
  | SubAgentSettled
  | UsageItem
  | ErrorItem
  | WarnItem;

type ToolTone = "read" | "write" | "bash" | "search" | "fetch" | "mcp";
const TOOL_GLYPH: Record<ToolTone, string> = {
  read: "▤",
  write: "▥",
  bash: "▶",
  search: "⊙",
  fetch: "⌬",
  mcp: "⊕",
};
const TOOL_COLOR: Record<ToolTone, string> = {
  read: BRAND,
  write: WARN,
  bash: ACCENT,
  search: BRAND,
  fetch: VIOLET,
  mcp: VIOLET,
};

function StaticRow({ item }: { item: StaticItem }): React.ReactElement {
  switch (item.kind) {
    case "user":
      return (
        <inkCompat.Box flexDirection="column" marginTop={1}>
          <inkCompat.Box flexDirection="row" gap={1}>
            <inkCompat.Text color={ACCENT}>›</inkCompat.Text>
            <inkCompat.Text>{item.text}</inkCompat.Text>
          </inkCompat.Box>
        </inkCompat.Box>
      );
    case "reasoning":
      return (
        <inkCompat.Box flexDirection="column" marginTop={1}>
          <inkCompat.Box flexDirection="row" gap={1}>
            <inkCompat.Text color={ACCENT}>◆</inkCompat.Text>
            <inkCompat.Text color={ACCENT} bold>
              reasoning
            </inkCompat.Text>
            <inkCompat.Text color={FAINT}>
              {`⋯ ${item.paragraphs} ¶ · ${item.tokens} tok · ${item.seconds.toFixed(1)}s · /reasoning last`}
            </inkCompat.Text>
          </inkCompat.Box>
          {item.tail.map((line, i) => (
            // biome-ignore lint/suspicious/noArrayIndexKey: tail preview, positional
            <inkCompat.Box key={`r-${i}`} paddingLeft={2}>
              <inkCompat.Text dimColor>{line}</inkCompat.Text>
            </inkCompat.Box>
          ))}
        </inkCompat.Box>
      );
    case "tool":
      return <ToolStaticRow item={item} />;
    case "plan":
      return (
        <inkCompat.Box flexDirection="column" marginTop={1}>
          <inkCompat.Box flexDirection="row" gap={1}>
            <inkCompat.Text color={ACCENT}>⊞</inkCompat.Text>
            <inkCompat.Text color={ACCENT} bold>
              plan
            </inkCompat.Text>
            <inkCompat.Text color={FAINT}>
              {`${item.steps.length} steps · ${item.seconds.toFixed(1)}s`}
            </inkCompat.Text>
          </inkCompat.Box>
          {item.steps.map((step) => (
            <inkCompat.Box key={step.label} paddingLeft={2} flexDirection="row" gap={1}>
              <inkCompat.Text color={planColor(step.status)}>
                {planGlyph(step.status)}
              </inkCompat.Text>
              <inkCompat.Text dimColor={step.status === "skipped"}>{step.label}</inkCompat.Text>
              {step.note ? <inkCompat.Text color={FAINT}>{`· ${step.note}`}</inkCompat.Text> : null}
            </inkCompat.Box>
          ))}
        </inkCompat.Box>
      );
    case "response": {
      const lines = item.text.split("\n");
      return (
        <inkCompat.Box flexDirection="column" marginTop={1}>
          <inkCompat.Box flexDirection="row" gap={1}>
            <inkCompat.Text color={OK}>‹</inkCompat.Text>
            <inkCompat.Text color={OK} bold>
              {item.aborted ? "response (truncated by esc)" : "response"}
            </inkCompat.Text>
          </inkCompat.Box>
          {lines.map((line, i) => (
            // biome-ignore lint/suspicious/noArrayIndexKey: response body lines are positional
            <inkCompat.Box key={`resp-${i}`} paddingLeft={2}>
              <inkCompat.Text>{line || " "}</inkCompat.Text>
            </inkCompat.Box>
          ))}
        </inkCompat.Box>
      );
    }
    case "diff":
      return (
        <inkCompat.Box flexDirection="column" marginTop={1}>
          <inkCompat.Box flexDirection="row" gap={1}>
            <inkCompat.Text color={WARN}>±</inkCompat.Text>
            <inkCompat.Text bold>{item.file}</inkCompat.Text>
            <inkCompat.Text color={OK}>{`+${item.added}`}</inkCompat.Text>
            <inkCompat.Text color={ERR}>{`-${item.removed}`}</inkCompat.Text>
          </inkCompat.Box>
          {item.preview.map((row, i) => (
            // biome-ignore lint/suspicious/noArrayIndexKey: diff preview lines are positional
            <inkCompat.Box key={`d-${i}`} paddingLeft={2} flexDirection="row" gap={1}>
              <inkCompat.Text color={row.kind === "+" ? OK : row.kind === "-" ? ERR : FAINT}>
                {row.kind}
              </inkCompat.Text>
              <inkCompat.Text dimColor={row.kind === " "}>{row.text}</inkCompat.Text>
            </inkCompat.Box>
          ))}
        </inkCompat.Box>
      );
    case "subagent":
      return (
        <inkCompat.Box flexDirection="column" marginTop={1}>
          <inkCompat.Box flexDirection="row" gap={1}>
            <inkCompat.Text color={item.ok ? VIOLET : ERR}>{item.ok ? "⌬" : "✖"}</inkCompat.Text>
            <inkCompat.Text color={VIOLET} bold>
              subagent
            </inkCompat.Text>
            <inkCompat.Text>{item.task}</inkCompat.Text>
            <inkCompat.Text color={FAINT}>{`· ${item.seconds.toFixed(1)}s`}</inkCompat.Text>
          </inkCompat.Box>
          {item.children.map((c, i) => (
            // biome-ignore lint/suspicious/noArrayIndexKey: subagent children are positional
            <inkCompat.Box key={`sub-${i}`} paddingLeft={2} flexDirection="row" gap={1}>
              <inkCompat.Text color={VIOLET}>▎</inkCompat.Text>
              <inkCompat.Text color={subChildColor(c.kind)}>{subChildGlyph(c.kind)}</inkCompat.Text>
              <inkCompat.Text dimColor>{c.summary}</inkCompat.Text>
            </inkCompat.Box>
          ))}
        </inkCompat.Box>
      );
    case "usage":
      return (
        <inkCompat.Box flexDirection="row" gap={2} marginTop={1}>
          <inkCompat.Text color={BRAND}>Σ</inkCompat.Text>
          <inkCompat.Text color={META}>
            {`in ${item.inputTokens} · out ${item.outputTokens} · $${item.totalCost.toFixed(4)}`}
          </inkCompat.Text>
        </inkCompat.Box>
      );
    case "error":
      return (
        <inkCompat.Box flexDirection="row" gap={1} marginTop={1}>
          <inkCompat.Text color={ERR}>✖</inkCompat.Text>
          <inkCompat.Text color={ERR}>{item.message}</inkCompat.Text>
        </inkCompat.Box>
      );
    case "warn":
      return (
        <inkCompat.Box flexDirection="row" gap={1} marginTop={1}>
          <inkCompat.Text color={WARN}>⚠</inkCompat.Text>
          <inkCompat.Text color={WARN}>{item.message}</inkCompat.Text>
        </inkCompat.Box>
      );
  }
}

function ToolStaticRow({ item }: { item: ToolSettled }): React.ReactElement {
  const glyph = item.status === "ok" ? "✓" : item.status === "rejected" ? "✗" : "✖";
  const headerColor =
    item.status === "ok" ? TOOL_COLOR[item.tone] : item.status === "rejected" ? FAINT : ERR;
  return (
    <inkCompat.Box flexDirection="column" marginTop={1}>
      <inkCompat.Box flexDirection="row" gap={1}>
        <inkCompat.Text color={headerColor}>{glyph}</inkCompat.Text>
        <inkCompat.Text color={headerColor}>{TOOL_GLYPH[item.tone]}</inkCompat.Text>
        <inkCompat.Text color={headerColor} bold>
          {item.name}
        </inkCompat.Text>
        <inkCompat.Text color={FAINT}>{item.args}</inkCompat.Text>
        {item.status === "rejected" ? (
          <inkCompat.Text color={ERR} bold>
            rejected
          </inkCompat.Text>
        ) : null}
        {item.status === "retry" && item.retryInfo ? (
          <inkCompat.Text color={WARN}>{`↻ ${item.retryInfo}`}</inkCompat.Text>
        ) : null}
        <inkCompat.Text color={FAINT}>{`${item.seconds.toFixed(1)}s`}</inkCompat.Text>
      </inkCompat.Box>
      {item.status === "rejected" ? null : (
        <>
          {item.hidden > 0 ? (
            <inkCompat.Box paddingLeft={2}>
              <inkCompat.Text color={FAINT}>{`⋮ ${item.hidden} earlier lines`}</inkCompat.Text>
            </inkCompat.Box>
          ) : null}
          {item.output.map((line, i) => (
            // biome-ignore lint/suspicious/noArrayIndexKey: tool tail output lines positional
            <inkCompat.Box key={`tool-${i}`} paddingLeft={2}>
              <inkCompat.Text dimColor color={item.status === "error" ? ERR : undefined}>
                {line || " "}
              </inkCompat.Text>
            </inkCompat.Box>
          ))}
        </>
      )}
    </inkCompat.Box>
  );
}

function planGlyph(status: PlanStep["status"]): string {
  switch (status) {
    case "todo":
      return "○";
    case "running":
      return "▶";
    case "done":
      return "✓";
    case "skipped":
      return "s";
    case "failed":
      return "✗";
    case "blocked":
      return "!";
  }
}
function planColor(status: PlanStep["status"]): string {
  switch (status) {
    case "todo":
      return PEND;
    case "running":
      return BRAND;
    case "done":
      return OK;
    case "skipped":
      return FAINT;
    case "failed":
      return ERR;
    case "blocked":
      return WARN;
  }
}
function subChildGlyph(kind: SubChild["kind"]): string {
  switch (kind) {
    case "reasoning":
      return "◆";
    case "tool":
      return "▶";
    case "diff":
      return "±";
    case "error":
      return "✖";
  }
}
function subChildColor(kind: SubChild["kind"]): string {
  switch (kind) {
    case "reasoning":
      return ACCENT;
    case "tool":
      return BRAND;
    case "diff":
      return WARN;
    case "error":
      return ERR;
  }
}

interface ReasoningActive {
  readonly kind: "reasoning";
  readonly tail: ReadonlyArray<string>;
  readonly tokens: number;
  readonly frame: number;
}
interface ToolActive {
  readonly kind: "tool";
  readonly tone: ToolTone;
  readonly name: string;
  readonly args: string;
  readonly outputLines: ReadonlyArray<string>;
  readonly elapsedMs: number;
  readonly frame: number;
}
interface PlanActive {
  readonly kind: "plan";
  readonly steps: ReadonlyArray<PlanStep>;
  readonly inProgressIdx: number;
  readonly frame: number;
}
interface ResponseActive {
  readonly kind: "response";
  readonly tail: ReadonlyArray<string>;
  readonly frame: number;
}
type ActiveCard = ReasoningActive | ToolActive | PlanActive | ResponseActive;

const REASONING_BURST = [
  "Looking at recent test failures in src/loop.test.ts.",
  "The assertion shape changed — expects a stripped trailing marker.",
  "But the new tokenizer in src/parser.ts keeps it.",
  "Two paths: patch tokenizer's strip step, or update the test expectation.",
  "The strip step is the safer fix; expectation matches user-visible output.",
  "Plan: rewire strip → run tests → if green, ship; otherwise update expectation.",
];

const SHELL_LONG = [
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
  "Test Suites: 9 passed, 9 total",
  "Tests:       142 passed, 142 total",
] as const;

const RESPONSE_TEXT =
  "The failing test is on src/loop.test.ts line 42 — the assertion expects the parser to drop the trailing tool-call marker, but the new tokenizer keeps it. Two paths forward: patch the tokenizer's strip step, or update the expectation. I'd patch the tokenizer — keeps existing tests green and matches user-visible output. Want me to draft the change?";

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
        <inkCompat.Text color={ACCENT}>{spin}</inkCompat.Text>
        <inkCompat.Text color={ACCENT} bold>
          reasoning
        </inkCompat.Text>
        <inkCompat.Text color={FAINT}>{`· ${card.tokens} tok`}</inkCompat.Text>
      </inkCompat.Box>
      {card.tail.map((line, i) => (
        // biome-ignore lint/suspicious/noArrayIndexKey: tail preview rotates by content, positional
        <inkCompat.Text key={`rl-${i}`} dimColor>
          {line}
        </inkCompat.Text>
      ))}
    </inkCompat.Box>
  );
}

function ToolActiveCard({ card }: { card: ToolActive }): React.ReactElement {
  const spin = SPINNER[card.frame % SPINNER.length] ?? "·";
  const tail = card.outputLines.slice(-5);
  const hidden = Math.max(0, card.outputLines.length - tail.length);
  const seconds = (card.elapsedMs / 1000).toFixed(1);
  const c = TOOL_COLOR[card.tone];
  return (
    <inkCompat.Box
      flexDirection="column"
      borderStyle="round"
      borderColor={c}
      paddingX={1}
      marginTop={1}
    >
      <inkCompat.Box flexDirection="row" gap={1}>
        <inkCompat.Text color={c}>{spin}</inkCompat.Text>
        <inkCompat.Text color={c}>{TOOL_GLYPH[card.tone]}</inkCompat.Text>
        <inkCompat.Text color={c} bold>
          {card.name}
        </inkCompat.Text>
        <inkCompat.Text color={FAINT}>{card.args}</inkCompat.Text>
        <inkCompat.Text color={FAINT}>{`· ${seconds}s`}</inkCompat.Text>
      </inkCompat.Box>
      {hidden > 0 ? (
        <inkCompat.Text color={FAINT}>{`⋮ ${hidden} earlier lines`}</inkCompat.Text>
      ) : null}
      {tail.map((line, i) => (
        // biome-ignore lint/suspicious/noArrayIndexKey: tool active tail positional
        <inkCompat.Text key={`ta-${i}`} dimColor>
          {line || " "}
        </inkCompat.Text>
      ))}
    </inkCompat.Box>
  );
}

function PlanActiveCard({ card }: { card: PlanActive }): React.ReactElement {
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
        <inkCompat.Text color={ACCENT}>⊞</inkCompat.Text>
        <inkCompat.Text color={ACCENT} bold>
          plan
        </inkCompat.Text>
      </inkCompat.Box>
      {card.steps.map((step, i) => {
        const running = i === card.inProgressIdx;
        const glyph = running ? spin : planGlyph(step.status);
        return (
          <inkCompat.Box key={step.label} flexDirection="row" gap={1}>
            <inkCompat.Text color={planColor(step.status)}>{glyph}</inkCompat.Text>
            <inkCompat.Text bold={running} dimColor={step.status === "todo" && !running}>
              {step.label}
            </inkCompat.Text>
            {step.note ? <inkCompat.Text color={FAINT}>{`· ${step.note}`}</inkCompat.Text> : null}
            {running ? <inkCompat.Text color={FAINT}>← in progress</inkCompat.Text> : null}
          </inkCompat.Box>
        );
      })}
    </inkCompat.Box>
  );
}

function ResponseActiveCard({ card }: { card: ResponseActive }): React.ReactElement {
  const spin = SPINNER[card.frame % SPINNER.length] ?? "·";
  return (
    <inkCompat.Box
      flexDirection="column"
      borderStyle="round"
      borderColor={OK}
      paddingX={1}
      marginTop={1}
    >
      <inkCompat.Box flexDirection="row" gap={1}>
        <inkCompat.Text color={OK}>{spin}</inkCompat.Text>
        <inkCompat.Text color={OK} bold>
          writing…
        </inkCompat.Text>
      </inkCompat.Box>
      {card.tail.map((line, i) => (
        // biome-ignore lint/suspicious/noArrayIndexKey: response tail positional
        <inkCompat.Text key={`rsp-${i}`}>{line || " "}</inkCompat.Text>
      ))}
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
      <inkCompat.Text color={META}>deepseek-r1</inkCompat.Text>
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
      <inkCompat.Text dimColor>card lifecycle reference · auto-replays · Esc exit</inkCompat.Text>
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
          startedRef.current = Date.now();
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
          // biome-ignore lint/suspicious/noArrayIndexKey: append-only history
          <StaticRow key={`h-${i}`} item={item} />
        ))}
      </inkCompat.Box>
      {active ? (
        active.kind === "reasoning" ? (
          <ReasoningCard card={active} />
        ) : active.kind === "tool" ? (
          <ToolActiveCard card={active} />
        ) : active.kind === "plan" ? (
          <PlanActiveCard card={active} />
        ) : (
          <ResponseActiveCard card={active} />
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

const sleep = (ms: number): Promise<void> => new Promise((r) => setTimeout(r, ms));

function buildScript(): Stage[] {
  return [
    async (_setActive, push) => {
      push({ kind: "user", text: "fix the failing test in src/loop.test.ts" });
      await sleep(400);
    },

    async (setActive, push) => {
      const start = Date.now();
      let frame = 0;
      let tokens = 0;
      let textRevealed = "";
      const fullText = REASONING_BURST.join(" ");
      const tick = setInterval(() => {
        frame++;
        const next = Math.min(fullText.length, textRevealed.length + 6);
        textRevealed = fullText.slice(0, next);
        tokens = Math.floor(textRevealed.length / 3);
        const lines = wrapText(textRevealed, 60);
        const tail = lines.slice(-4);
        setActive({ kind: "reasoning", tail, tokens, frame });
      }, 60);
      await sleep(2400);
      clearInterval(tick);
      const seconds = (Date.now() - start) / 1000;
      const lines = wrapText(textRevealed, 60);
      setActive(null);
      push({
        kind: "reasoning",
        tail: lines.slice(-2),
        paragraphs: REASONING_BURST.length,
        tokens,
        seconds,
      });
      await sleep(300);
    },

    async (setActive, push) => {
      const start = Date.now();
      const target = ["import { Loop } from './loop.js';", "", "test('parser strips trailer', …"];
      let frame = 0;
      const tick = setInterval(() => {
        frame++;
        setActive({
          kind: "tool",
          tone: "read",
          name: "read_file",
          args: "src/loop.test.ts",
          outputLines: target,
          elapsedMs: Date.now() - start,
          frame,
        });
      }, 80);
      await sleep(700);
      clearInterval(tick);
      setActive(null);
      push({
        kind: "tool",
        tone: "read",
        name: "read_file",
        args: "src/loop.test.ts",
        output: target.slice(-2),
        hidden: 38,
        seconds: (Date.now() - start) / 1000,
        status: "ok",
      });
      await sleep(200);
    },

    async (setActive, push) => {
      const start = Date.now();
      let frame = 0;
      const lines: string[] = [];
      const spinTimer = setInterval(() => {
        frame++;
        setActive({
          kind: "tool",
          tone: "bash",
          name: "bash",
          args: "npm test",
          outputLines: lines.slice(),
          elapsedMs: Date.now() - start,
          frame,
        });
      }, 80);
      const lineTimer = setInterval(() => {
        if (lines.length < SHELL_LONG.length) {
          lines.push(SHELL_LONG[lines.length] ?? "");
        }
      }, 240);
      await sleep(SHELL_LONG.length * 240 + 400);
      clearInterval(spinTimer);
      clearInterval(lineTimer);
      setActive(null);
      push({
        kind: "tool",
        tone: "bash",
        name: "bash",
        args: "npm test",
        output: SHELL_LONG.slice(-5),
        hidden: SHELL_LONG.length - 5,
        seconds: (Date.now() - start) / 1000,
        status: "ok",
      });
      await sleep(300);
    },

    async (_setActive, push) => {
      push({
        kind: "tool",
        tone: "bash",
        name: "bash",
        args: "npm run lint",
        output: ["error: command not found: biome", "exit code 127"],
        hidden: 0,
        seconds: 0.4,
        status: "retry",
        retryInfo: "retry 1/3",
      });
      await sleep(500);
    },

    async (setActive, push) => {
      const start = Date.now();
      const labels: PlanStep[] = [
        { status: "todo", label: "patch tokenizer.strip()" },
        { status: "todo", label: "rerun npm test" },
        { status: "todo", label: "rebuild dist", note: "skip if cached" },
        { status: "todo", label: "publish patch" },
      ];
      const steps = labels.slice();
      let frame = 0;
      let cur = 0;
      const tick = setInterval(() => {
        frame++;
        setActive({ kind: "plan", steps: steps.slice(), inProgressIdx: cur, frame });
      }, 80);
      while (cur < steps.length) {
        await sleep(750);
        steps[cur] = { ...steps[cur]!, status: "done" };
        cur++;
      }
      clearInterval(tick);
      setActive(null);
      push({
        kind: "plan",
        steps: steps.slice(),
        seconds: (Date.now() - start) / 1000,
      });
      await sleep(300);
    },

    async (_setActive, push) => {
      push({
        kind: "tool",
        tone: "write",
        name: "write_file",
        args: "src/parser.ts (12+ 3-)",
        output: [],
        hidden: 0,
        seconds: 0.0,
        status: "rejected",
      });
      await sleep(400);
    },

    async (_setActive, push) => {
      push({
        kind: "diff",
        file: "src/parser.ts",
        added: 12,
        removed: 3,
        preview: [
          { kind: " ", text: "function strip(token: string) {" },
          { kind: "-", text: "  return token.trimEnd();" },
          { kind: "+", text: "  if (token.endsWith(TRAILER)) {" },
          { kind: "+", text: "    return token.slice(0, -TRAILER.length);" },
          { kind: "+", text: "  }" },
          { kind: "+", text: "  return token;" },
          { kind: " ", text: "}" },
        ],
      });
      await sleep(500);
    },

    async (_setActive, push) => {
      push({
        kind: "subagent",
        task: "investigate flaky tokenizer regression",
        seconds: 8.4,
        ok: true,
        children: [
          { kind: "reasoning", summary: "scanning the parser fixtures" },
          { kind: "tool", summary: "grep TRAILER src/" },
          { kind: "tool", summary: "read src/parser.ts" },
          { kind: "diff", summary: "src/parser.ts +12 -3" },
        ],
      });
      await sleep(400);
    },

    async (setActive, push) => {
      let frame = 0;
      let revealed = 0;
      const tick = setInterval(() => {
        frame++;
        revealed = Math.min(RESPONSE_TEXT.length, revealed + 6);
        const lines = wrapText(RESPONSE_TEXT.slice(0, revealed), 70);
        setActive({ kind: "response", tail: lines.slice(-4), frame });
      }, 33);
      while (revealed < RESPONSE_TEXT.length) await sleep(50);
      clearInterval(tick);
      setActive(null);
      push({ kind: "response", text: RESPONSE_TEXT });
      await sleep(500);
    },

    async (_setActive, push) => {
      push({
        kind: "usage",
        inputTokens: 1842,
        outputTokens: 421,
        totalCost: 0.0094,
      });
      await sleep(400);
    },

    async (_setActive, push) => {
      push({ kind: "warn", message: "context budget at 73% · /compact suggested" });
      await sleep(400);
    },

    async (_setActive, push) => {
      push({ kind: "error", message: "rate-limit hit on next request · retrying in 3s…" });
      await sleep(700);
    },
  ];
}

function wrapText(text: string, width: number): string[] {
  const lines: string[] = [];
  for (const para of text.split("\n")) {
    if (para.length <= width) {
      lines.push(para);
      continue;
    }
    const words = para.split(" ");
    let cur = "";
    for (const w of words) {
      if (cur.length + w.length + 1 > width) {
        if (cur) lines.push(cur);
        cur = w;
      } else {
        cur = cur ? `${cur} ${w}` : w;
      }
    }
    if (cur) lines.push(cur);
  }
  return lines;
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
