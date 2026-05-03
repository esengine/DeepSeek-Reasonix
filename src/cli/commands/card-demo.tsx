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

type ToolTone = "read" | "write" | "bash" | "search" | "fetch" | "mcp" | "patch";

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
  readonly aborted?: boolean;
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
  readonly lines: ReadonlyArray<{ kind: "text" | "code" | "header" | "list"; text: string }>;
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

const TOOL_GLYPH: Record<ToolTone, string> = {
  read: "▤",
  write: "▥",
  bash: "▶",
  search: "⊙",
  fetch: "⌬",
  mcp: "⊕",
  patch: "✎",
};
const TOOL_COLOR: Record<ToolTone, string> = {
  read: BRAND,
  write: WARN,
  bash: ACCENT,
  search: BRAND,
  fetch: VIOLET,
  mcp: VIOLET,
  patch: WARN,
};
const TOOL_TAIL_LEN: Record<ToolTone, number> = {
  read: 2,
  write: 2,
  bash: 5,
  search: 2,
  fetch: 2,
  mcp: 5,
  patch: 2,
};

interface ReasoningActive {
  readonly id: string;
  readonly kind: "reasoning";
  readonly tail: ReadonlyArray<string>;
  readonly tokens: number;
  readonly frame: number;
  readonly aborted?: boolean;
}
interface ToolActive {
  readonly id: string;
  readonly kind: "tool";
  readonly tone: ToolTone;
  readonly name: string;
  readonly args: string;
  readonly outputLines: ReadonlyArray<string>;
  readonly elapsedMs: number;
  readonly frame: number;
  readonly retry?: { attempt: number; max: number };
}
interface PlanActive {
  readonly id: string;
  readonly kind: "plan";
  readonly title: string;
  readonly steps: ReadonlyArray<PlanStep>;
  readonly inProgressIdx: number | null;
  readonly frame: number;
}
interface SubAgentActive {
  readonly id: string;
  readonly kind: "subagent";
  readonly task: string;
  readonly children: ReadonlyArray<SubAgentChild>;
  readonly frame: number;
}
interface SubAgentChild {
  readonly status: "running" | "done";
  readonly kind: "reasoning" | "tool" | "diff";
  readonly summary: string;
  readonly tone?: ToolTone;
}
interface ResponseActive {
  readonly id: string;
  readonly kind: "response";
  readonly tail: ReadonlyArray<string>;
  readonly frame: number;
}
type ActiveCard = ReasoningActive | ToolActive | PlanActive | SubAgentActive | ResponseActive;

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
            <inkCompat.Text color={item.aborted ? ERR : ACCENT}>◆</inkCompat.Text>
            <inkCompat.Text color={item.aborted ? ERR : ACCENT} bold>
              {item.aborted ? "reasoning (aborted)" : "reasoning"}
            </inkCompat.Text>
            <inkCompat.Text color={FAINT}>
              {`${item.paragraphs}¶ · ${item.tokens} tok · ${item.seconds.toFixed(1)}s`}
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
    case "response":
      return (
        <inkCompat.Box flexDirection="column" marginTop={1}>
          <inkCompat.Box flexDirection="row" gap={1}>
            <inkCompat.Text color={item.aborted ? ERR : OK}>‹</inkCompat.Text>
            <inkCompat.Text color={item.aborted ? ERR : OK} bold>
              {item.aborted ? "response (truncated by esc)" : "response"}
            </inkCompat.Text>
          </inkCompat.Box>
          {item.lines.map((line, i) => (
            // biome-ignore lint/suspicious/noArrayIndexKey: response body lines positional
            <inkCompat.Box key={`resp-${i}`} paddingLeft={2}>
              {line.kind === "code" ? (
                <inkCompat.Text color={BRAND}>{line.text || " "}</inkCompat.Text>
              ) : line.kind === "header" ? (
                <inkCompat.Text bold>{line.text || " "}</inkCompat.Text>
              ) : line.kind === "list" ? (
                <inkCompat.Text>{`  • ${line.text}`}</inkCompat.Text>
              ) : (
                <inkCompat.Text>{line.text || " "}</inkCompat.Text>
              )}
            </inkCompat.Box>
          ))}
        </inkCompat.Box>
      );
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
            // biome-ignore lint/suspicious/noArrayIndexKey: diff preview lines positional
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
            // biome-ignore lint/suspicious/noArrayIndexKey: subagent children positional
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
          <inkCompat.Text color={WARN}>{`R ${item.retryInfo}`}</inkCompat.Text>
        ) : null}
        <inkCompat.Text color={FAINT}>{`${item.seconds.toFixed(1)}s`}</inkCompat.Text>
      </inkCompat.Box>
      {item.status === "rejected" ? null : (
        <>
          {item.hidden > 0 ? (
            <inkCompat.Box paddingLeft={2}>
              <inkCompat.Text color={FAINT}>{`: ${item.hidden} earlier lines`}</inkCompat.Text>
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
  const tailLen = TOOL_TAIL_LEN[card.tone];
  const tail = card.outputLines.slice(-tailLen);
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
        {card.retry ? (
          <inkCompat.Text color={WARN}>
            {`R ${card.retry.attempt}/${card.retry.max}`}
          </inkCompat.Text>
        ) : null}
        <inkCompat.Text color={FAINT}>{`· ${seconds}s`}</inkCompat.Text>
      </inkCompat.Box>
      {hidden > 0 ? (
        <inkCompat.Text color={FAINT}>{`: ${hidden} earlier lines`}</inkCompat.Text>
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
  const done = card.steps.filter((s) => s.status === "done" || s.status === "skipped").length;
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
          {card.title}
        </inkCompat.Text>
        <inkCompat.Text color={FAINT}>{`${done} of ${card.steps.length} done`}</inkCompat.Text>
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

function SubAgentActiveCard({ card }: { card: SubAgentActive }): React.ReactElement {
  const spin = SPINNER[card.frame % SPINNER.length] ?? "·";
  const runningCount = card.children.filter((c) => c.status === "running").length;
  return (
    <inkCompat.Box
      flexDirection="column"
      borderStyle="round"
      borderColor={VIOLET}
      paddingX={1}
      marginTop={1}
    >
      <inkCompat.Box flexDirection="row" gap={1}>
        <inkCompat.Text color={VIOLET}>{spin}</inkCompat.Text>
        <inkCompat.Text color={VIOLET} bold>
          subagent
        </inkCompat.Text>
        <inkCompat.Text>{card.task}</inkCompat.Text>
        <inkCompat.Text color={FAINT}>{`${runningCount} running`}</inkCompat.Text>
      </inkCompat.Box>
      {card.children.map((c, i) => {
        const cglyph = c.status === "running" ? spin : "✓";
        const ccolor = c.status === "running" ? BRAND : OK;
        return (
          <inkCompat.Box
            // biome-ignore lint/suspicious/noArrayIndexKey: subagent active children positional
            key={`sc-${i}`}
            flexDirection="row"
            gap={1}
          >
            <inkCompat.Text color={VIOLET}>▎</inkCompat.Text>
            <inkCompat.Text color={ccolor}>{cglyph}</inkCompat.Text>
            <inkCompat.Text color={subChildColor(c.kind)}>{subChildGlyph(c.kind)}</inkCompat.Text>
            <inkCompat.Text dimColor={c.status === "done"}>{c.summary}</inkCompat.Text>
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

function ActiveCardView({ card }: { card: ActiveCard }): React.ReactElement {
  switch (card.kind) {
    case "reasoning":
      return <ReasoningCard card={card} />;
    case "tool":
      return <ToolActiveCard card={card} />;
    case "plan":
      return <PlanActiveCard card={card} />;
    case "subagent":
      return <SubAgentActiveCard card={card} />;
    case "response":
      return <ResponseActiveCard card={card} />;
  }
}

function StatusRow({ elapsedMs, cost }: { elapsedMs: number; cost: number }): React.ReactElement {
  const seconds = (elapsedMs / 1000).toFixed(1);
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

interface DemoApi {
  setActive(updater: (prev: readonly ActiveCard[]) => readonly ActiveCard[]): void;
  push(item: StaticItem): void;
  reset(): void;
  cancelled: () => boolean;
  sleep(ms: number): Promise<void>;
}

export function CardDemoShell({ onExit }: ShellProps): React.ReactElement {
  const [history, setHistory] = useState<readonly StaticItem[]>([]);
  const [active, setActive] = useState<readonly ActiveCard[]>([]);
  const [elapsed, setElapsed] = useState(0);
  const [cost, setCost] = useState(0);
  const startedRef = useRef(Date.now());

  useKeystroke((k) => {
    if (k.escape) onExit();
  });

  useEffect(() => {
    const id = setInterval(() => {
      const now = Date.now() - startedRef.current;
      setElapsed(now);
      setCost((now / 1000) * 0.0008);
    }, 100);
    return () => clearInterval(id);
  }, []);

  useEffect(() => {
    let cancelled = false;
    const api: DemoApi = {
      setActive: (updater) => {
        if (cancelled) return;
        setActive((prev) => updater(prev));
      },
      push: (item) => {
        if (cancelled) return;
        setHistory((prev) => [...prev, item]);
      },
      reset: () => {
        if (cancelled) return;
        setActive([]);
        setHistory([]);
        startedRef.current = Date.now();
      },
      cancelled: () => cancelled,
      sleep: (ms) => new Promise((r) => setTimeout(r, ms)),
    };
    void runScript(api);
    return () => {
      cancelled = true;
    };
  }, []);

  return (
    <inkCompat.Box flexDirection="column">
      <StatusRow elapsedMs={elapsed} cost={cost} />
      <inkCompat.Box flexDirection="column">
        {history.map((item, i) => (
          // biome-ignore lint/suspicious/noArrayIndexKey: append-only history
          <StaticRow key={`h-${i}`} item={item} />
        ))}
      </inkCompat.Box>
      {active.map((card, i) => (
        // biome-ignore lint/suspicious/noArrayIndexKey: live region cards positional
        <ActiveCardView key={`a-${i}-${card.id}`} card={card} />
      ))}
      {active.length === 0 ? <PromptInput /> : null}
      <HintBar />
    </inkCompat.Box>
  );
}

function replaceById(
  cards: readonly ActiveCard[],
  id: string,
  next: ActiveCard | null,
): readonly ActiveCard[] {
  if (next === null) return cards.filter((c) => c.id !== id);
  return cards.map((c) => (c.id === id ? next : c));
}

function appendCard(cards: readonly ActiveCard[], card: ActiveCard): readonly ActiveCard[] {
  return [...cards, card];
}

async function runScript(api: DemoApi): Promise<void> {
  while (!api.cancelled()) {
    await sceneFixFailingTest(api);
    if (api.cancelled()) return;
    await api.sleep(1000);
    if (api.cancelled()) return;
    await sceneSubagent(api);
    if (api.cancelled()) return;
    await api.sleep(800);
    if (api.cancelled()) return;
    await sceneEdgeCases(api);
    if (api.cancelled()) return;
    await api.sleep(1500);
    api.reset();
    await api.sleep(500);
  }
}

async function streamReasoning(
  api: DemoApi,
  fullText: string,
  durationMs: number,
  abortAtMs?: number,
): Promise<{ tokens: number; lines: string[]; aborted: boolean }> {
  const id = "reason";
  const start = Date.now();
  let frame = 0;
  let revealed = "";
  let aborted = false;
  api.setActive((prev) => appendCard(prev, { id, kind: "reasoning", tail: [], tokens: 0, frame }));
  while (Date.now() - start < durationMs && !api.cancelled()) {
    if (abortAtMs !== undefined && Date.now() - start >= abortAtMs) {
      aborted = true;
      break;
    }
    await api.sleep(60);
    frame++;
    revealed = fullText.slice(0, Math.min(fullText.length, revealed.length + 6));
    const lines = wrapText(revealed, 70);
    const tail = lines.slice(-4);
    const tokens = Math.floor(revealed.length / 3);
    const aborting = abortAtMs !== undefined && Date.now() - start >= abortAtMs - 100;
    api.setActive((prev) =>
      replaceById(prev, id, {
        id,
        kind: "reasoning",
        tail,
        tokens,
        frame,
        aborted: aborting,
      }),
    );
  }
  api.setActive((prev) => replaceById(prev, id, null));
  const lines = wrapText(revealed, 70);
  return { tokens: Math.floor(revealed.length / 3), lines, aborted };
}

async function streamTool(
  api: DemoApi,
  config: {
    tone: ToolTone;
    name: string;
    args: string;
    output: ReadonlyArray<string>;
    durationMs: number;
    retry?: { attempt: number; max: number };
  },
): Promise<void> {
  const id = `tool-${Math.random().toString(36).slice(2)}`;
  const start = Date.now();
  let frame = 0;
  const out: string[] = [];
  api.setActive((prev) =>
    appendCard(prev, {
      id,
      kind: "tool",
      tone: config.tone,
      name: config.name,
      args: config.args,
      outputLines: out,
      elapsedMs: 0,
      frame,
      retry: config.retry,
    }),
  );
  const lineRate = config.durationMs / Math.max(1, config.output.length);
  let lineIdx = 0;
  while (Date.now() - start < config.durationMs && !api.cancelled()) {
    await api.sleep(80);
    frame++;
    const elapsed = Date.now() - start;
    while (lineIdx < config.output.length && elapsed > lineIdx * lineRate) {
      out.push(config.output[lineIdx] ?? "");
      lineIdx++;
    }
    api.setActive((prev) =>
      replaceById(prev, id, {
        id,
        kind: "tool",
        tone: config.tone,
        name: config.name,
        args: config.args,
        outputLines: out.slice(),
        elapsedMs: elapsed,
        frame,
        retry: config.retry,
      }),
    );
  }
  api.setActive((prev) => replaceById(prev, id, null));
}

async function sceneFixFailingTest(api: DemoApi): Promise<void> {
  api.push({ kind: "user", text: "fix the failing test in src/loop.test.ts" });
  await api.sleep(400);
  if (api.cancelled()) return;

  // 1. reasoning
  const reasoningText =
    "Looking at recent test failures in src/loop.test.ts. The assertion shape changed -- expects a stripped trailing marker. The new tokenizer in src/parser.ts keeps it. Two paths: patch tokenizer's strip step, or update the test expectation.";
  const reasonStart = Date.now();
  const r = await streamReasoning(api, reasoningText, 2200);
  if (api.cancelled()) return;
  api.push({
    kind: "reasoning",
    tail: r.lines.slice(-2),
    paragraphs: 3,
    tokens: r.tokens,
    seconds: (Date.now() - reasonStart) / 1000,
  });
  await api.sleep(200);

  // 2. plan executes step-by-step
  const planId = "plan-main";
  const planStart = Date.now();
  let frame = 0;
  const steps: PlanStep[] = [
    { status: "todo", label: "locate failing assertion" },
    { status: "todo", label: "patch tokenizer.strip()" },
    { status: "todo", label: "verify with npm test" },
    { status: "todo", label: "publish patch" },
  ];

  const renderPlan = (inProgressIdx: number | null) => {
    api.setActive((prev) =>
      replaceById(
        prev.find((c) => c.id === planId) ? prev : appendCard(prev, makePlan()),
        planId,
        makePlan(),
      ),
    );
    function makePlan(): PlanActive {
      return {
        id: planId,
        kind: "plan",
        title: "fix loop test",
        steps: steps.slice(),
        inProgressIdx,
        frame,
      };
    }
  };

  // ensure plan card mounted
  api.setActive((prev) =>
    appendCard(prev, {
      id: planId,
      kind: "plan",
      title: "fix loop test",
      steps: steps.slice(),
      inProgressIdx: 0,
      frame,
    }),
  );

  // local frame tick for plan spinner while sub-tools run
  const planTick = setInterval(() => {
    frame++;
    api.setActive((prev) => {
      const cur = prev.find((c) => c.id === planId);
      if (!cur || cur.kind !== "plan") return prev;
      return replaceById(prev, planId, { ...cur, frame });
    });
  }, 80);

  try {
    // step 0: search
    steps[0] = { ...steps[0]!, status: "running" };
    renderPlan(0);
    await streamTool(api, {
      tone: "search",
      name: "grep",
      args: "TRAILER src/parser.ts",
      output: [
        "src/parser.ts:42:const TRAILER = '<|/tool|>';",
        "src/parser.ts:67:  if (s.endsWith(TRAILER))",
      ],
      durationMs: 800,
    });
    if (api.cancelled()) return;
    api.push({
      kind: "tool",
      tone: "search",
      name: "grep",
      args: "TRAILER src/parser.ts",
      output: ["src/parser.ts:67:  if (s.endsWith(TRAILER))"],
      hidden: 1,
      seconds: 0.8,
      status: "ok",
    });
    steps[0] = { ...steps[0]!, status: "done" };
    renderPlan(1);
    await api.sleep(300);
    if (api.cancelled()) return;

    // step 1: write file (rejected)
    steps[1] = { ...steps[1]!, status: "running" };
    renderPlan(1);
    await streamTool(api, {
      tone: "write",
      name: "write_file",
      args: "src/parser.ts (full rewrite)",
      output: [],
      durationMs: 600,
    });
    if (api.cancelled()) return;
    api.push({
      kind: "tool",
      tone: "write",
      name: "write_file",
      args: "src/parser.ts (full rewrite)",
      output: [],
      hidden: 0,
      seconds: 0.6,
      status: "rejected",
    });
    steps[1] = { ...steps[1]!, status: "blocked", note: "rejected; trying patch instead" };
    renderPlan(null);
    await api.sleep(800);
    if (api.cancelled()) return;

    // step 1 retry: patch (smaller)
    steps[1] = { ...steps[1]!, status: "running", note: undefined };
    renderPlan(1);
    await streamTool(api, {
      tone: "patch",
      name: "edit_file",
      args: "src/parser.ts -3+12",
      output: [
        "applying hunk 1/1",
        "--- src/parser.ts",
        "+++ src/parser.ts",
        "@@ -42,3 +42,12 @@",
        "+function strip(token: string) {...}",
      ],
      durationMs: 1100,
    });
    if (api.cancelled()) return;
    api.push({
      kind: "tool",
      tone: "patch",
      name: "edit_file",
      args: "src/parser.ts -3+12",
      output: ["+function strip(token: string) {...}"],
      hidden: 4,
      seconds: 1.1,
      status: "ok",
    });
    steps[1] = { ...steps[1]!, status: "done" };
    renderPlan(2);
    await api.sleep(300);
    if (api.cancelled()) return;

    // step 2: bash with retry chain
    steps[2] = { ...steps[2]!, status: "running" };
    renderPlan(2);

    const bashOutput = [
      " RUNS  src/loop.test.ts",
      " FAIL  src/loop.test.ts",
      "  expect(received).toBe(expected)",
      "  - expected:  '<final>'",
      "  + received:  '<final><|/tool|>'",
    ];
    await streamTool(api, {
      tone: "bash",
      name: "bash",
      args: "npm test",
      output: bashOutput,
      durationMs: 900,
      retry: { attempt: 1, max: 3 },
    });
    if (api.cancelled()) return;
    api.push({
      kind: "tool",
      tone: "bash",
      name: "bash",
      args: "npm test",
      output: bashOutput.slice(-5),
      hidden: 0,
      seconds: 0.9,
      status: "retry",
      retryInfo: "1/3",
    });
    await api.sleep(200);

    // retry 2 — succeed
    const bashOk = [
      " PASS  src/loop.test.ts",
      " PASS  src/parser.test.ts",
      " PASS  src/diff/cell.test.ts",
      " PASS  src/diff/screen.test.ts",
      " PASS  src/renderer/layout.test.ts",
      "Tests:       142 passed",
    ];
    await streamTool(api, {
      tone: "bash",
      name: "bash",
      args: "npm test",
      output: bashOk,
      durationMs: 1300,
      retry: { attempt: 2, max: 3 },
    });
    if (api.cancelled()) return;
    api.push({
      kind: "tool",
      tone: "bash",
      name: "bash",
      args: "npm test",
      output: bashOk.slice(-5),
      hidden: 1,
      seconds: 1.3,
      status: "ok",
    });
    steps[2] = { ...steps[2]!, status: "done" };
    renderPlan(3);
    await api.sleep(300);
    if (api.cancelled()) return;

    // step 3: bash deploy
    steps[3] = { ...steps[3]!, status: "running" };
    renderPlan(3);
    await streamTool(api, {
      tone: "bash",
      name: "bash",
      args: "npm run build && npm publish",
      output: [
        "  > tsup",
        "  ESM Build start",
        "  CJS Build start",
        "  DTS Build start",
        "  ESM done",
        "  + reasonix@0.24.0",
      ],
      durationMs: 1500,
    });
    if (api.cancelled()) return;
    api.push({
      kind: "tool",
      tone: "bash",
      name: "bash",
      args: "npm run build && npm publish",
      output: ["  + reasonix@0.24.0"],
      hidden: 5,
      seconds: 1.5,
      status: "ok",
    });
    steps[3] = { ...steps[3]!, status: "done" };
    renderPlan(null);
  } finally {
    clearInterval(planTick);
  }

  // settle plan
  await api.sleep(400);
  api.setActive((prev) => replaceById(prev, planId, null));
  api.push({ kind: "plan", steps: steps.slice(), seconds: (Date.now() - planStart) / 1000 });
  await api.sleep(300);
  if (api.cancelled()) return;

  // diff
  api.push({
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
  await api.sleep(400);
  if (api.cancelled()) return;

  // streaming response with markdown
  await streamMarkdownResponse(api);
}

async function streamMarkdownResponse(api: DemoApi): Promise<void> {
  const lines: { kind: "text" | "code" | "header" | "list"; text: string }[] = [
    { kind: "header", text: "## Patch landed" },
    { kind: "text", text: "Tests pass on second retry. The patch:" },
    { kind: "text", text: "" },
    { kind: "code", text: "function strip(token: string) {" },
    { kind: "code", text: "  if (token.endsWith(TRAILER))" },
    { kind: "code", text: "    return token.slice(0, -TRAILER.length);" },
    { kind: "code", text: "  return token;" },
    { kind: "code", text: "}" },
    { kind: "text", text: "" },
    { kind: "list", text: "checks the suffix before slicing — no spurious slices on plain tokens" },
    { kind: "list", text: "released as 0.24.0 to npm" },
  ];
  const id = "resp";
  let frame = 0;
  let revealed = 0;
  api.setActive((prev) => appendCard(prev, { id, kind: "response", tail: [], frame }));
  while (revealed < lines.length && !api.cancelled()) {
    await api.sleep(160);
    frame++;
    revealed = Math.min(lines.length, revealed + 1);
    const tail = lines
      .slice(0, revealed)
      .slice(-4)
      .map((l) =>
        l.kind === "code" ? `  ${l.text}` : l.kind === "list" ? `  - ${l.text}` : l.text,
      );
    api.setActive((prev) => replaceById(prev, id, { id, kind: "response", tail, frame }));
  }
  api.setActive((prev) => replaceById(prev, id, null));
  api.push({ kind: "response", lines });
  await api.sleep(200);
  if (api.cancelled()) return;
  api.push({ kind: "usage", inputTokens: 1842, outputTokens: 421, totalCost: 0.0094 });
}

async function sceneSubagent(api: DemoApi): Promise<void> {
  api.push({ kind: "user", text: "investigate the auth flow regressions in 0.24" });
  await api.sleep(400);
  if (api.cancelled()) return;

  const id = "sub";
  let frame = 0;
  const start = Date.now();
  const children: SubAgentChild[] = [
    { status: "running", kind: "reasoning", summary: "scanning auth fixtures" },
    { status: "running", kind: "tool", summary: "grep 'authToken' src/", tone: "search" },
    { status: "running", kind: "tool", summary: "read src/auth/session.ts", tone: "read" },
  ];
  api.setActive((prev) =>
    appendCard(prev, {
      id,
      kind: "subagent",
      task: "auth regressions",
      children: children.slice(),
      frame,
    }),
  );
  const tick = setInterval(() => {
    frame++;
    api.setActive((prev) => {
      const cur = prev.find((c) => c.id === id);
      if (!cur || cur.kind !== "subagent") return prev;
      return replaceById(prev, id, { ...cur, frame, children: children.slice() });
    });
  }, 80);

  try {
    await api.sleep(1200);
    if (api.cancelled()) return;
    children[0] = { ...children[0]!, status: "done" };
    await api.sleep(900);
    if (api.cancelled()) return;
    children[1] = { ...children[1]!, status: "done" };
    await api.sleep(1100);
    if (api.cancelled()) return;
    children[2] = { ...children[2]!, status: "done" };
    children.push({ status: "running", kind: "diff", summary: "patching session.ts" });
    await api.sleep(1000);
    if (api.cancelled()) return;
    children[3] = { ...children[3]!, status: "done" };
    await api.sleep(400);
  } finally {
    clearInterval(tick);
  }

  api.setActive((prev) => replaceById(prev, id, null));
  api.push({
    kind: "subagent",
    task: "auth regressions",
    children: [
      { kind: "reasoning", summary: "scanned 14 fixtures, found 2 stale" },
      { kind: "tool", summary: "grep 'authToken' src/" },
      { kind: "tool", summary: "read src/auth/session.ts" },
      { kind: "diff", summary: "src/auth/session.ts +5 -2" },
    ],
    seconds: (Date.now() - start) / 1000,
    ok: true,
  });
  await api.sleep(400);
  if (api.cancelled()) return;

  // CJK streaming response
  const id2 = "resp-cjk";
  const text =
    "我已经定位到问题：session.ts 的 token 续期逻辑在 0.24 改了顺序，导致旧 token 在 refresh 之前就被清掉。修了，再跑测试都过了。";
  const lines: { kind: "text" | "code" | "header" | "list"; text: string }[] = [
    { kind: "header", text: "结论" },
    { kind: "text", text },
    { kind: "list", text: "已修复，提交在 src/auth/session.ts" },
  ];
  let frame2 = 0;
  let revealed2 = 0;
  api.setActive((prev) => appendCard(prev, { id: id2, kind: "response", tail: [], frame: frame2 }));
  while (revealed2 < lines.length && !api.cancelled()) {
    await api.sleep(180);
    frame2++;
    revealed2++;
    const tail = lines
      .slice(0, revealed2)
      .slice(-4)
      .map((l) => (l.kind === "list" ? `  - ${l.text}` : l.text));
    api.setActive((prev) =>
      replaceById(prev, id2, { id: id2, kind: "response", tail, frame: frame2 }),
    );
  }
  api.setActive((prev) => replaceById(prev, id2, null));
  api.push({ kind: "response", lines });
}

async function sceneEdgeCases(api: DemoApi): Promise<void> {
  api.push({ kind: "user", text: "summarize the deploy log" });
  await api.sleep(400);
  if (api.cancelled()) return;

  // aborted reasoning
  const reasoningText =
    "Pulling deploy logs from the last hour. Looking for warnings and errors. The database connection retried 3 times before stabilizing...";
  const reasonStart = Date.now();
  const r = await streamReasoning(api, reasoningText, 1200, 800);
  if (api.cancelled()) return;
  api.push({
    kind: "reasoning",
    tail: r.lines.slice(-2),
    paragraphs: 1,
    tokens: r.tokens,
    seconds: (Date.now() - reasonStart) / 1000,
    aborted: true,
  });
  await api.sleep(300);
  if (api.cancelled()) return;

  // mcp tool
  await streamTool(api, {
    tone: "mcp",
    name: "mcp.linear.search_issues",
    args: '{"query":"deploy error","limit":5}',
    output: [
      "INC-2871 — db pool exhausted on retry",
      "INC-2872 — auth refresh race",
      "INC-2873 — log shipper timeout",
    ],
    durationMs: 1100,
  });
  if (api.cancelled()) return;
  api.push({
    kind: "tool",
    tone: "mcp",
    name: "mcp.linear.search_issues",
    args: '{"query":"deploy error","limit":5}',
    output: ["INC-2873 — log shipper timeout"],
    hidden: 2,
    seconds: 1.1,
    status: "ok",
  });
  await api.sleep(300);
  if (api.cancelled()) return;

  // long fetch
  await streamTool(api, {
    tone: "fetch",
    name: "web_fetch",
    args: "https://status.deepseek.com/api/incidents",
    output: ["status: 200", '{"incidents":[]}'],
    durationMs: 700,
  });
  if (api.cancelled()) return;
  api.push({
    kind: "tool",
    tone: "fetch",
    name: "web_fetch",
    args: "https://status.deepseek.com/api/incidents",
    output: ["status: 200", '{"incidents":[]}'],
    hidden: 0,
    seconds: 0.7,
    status: "ok",
  });
  await api.sleep(300);
  if (api.cancelled()) return;

  // warn + error
  api.push({ kind: "warn", message: "context budget at 73% · /compact suggested" });
  await api.sleep(400);
  api.push({ kind: "error", message: "rate-limit hit · backing off 8s" });
  await api.sleep(500);
  api.push({ kind: "usage", inputTokens: 487, outputTokens: 96, totalCost: 0.0021 });
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
