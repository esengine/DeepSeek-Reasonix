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
const PEND = "#484f58";

const SPINNER_FRAMES = ["⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"] as const;
const SHELL_LINES = [
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
const RESPONSE = [
  "Working through the failing test on src/loop.test.ts.",
  "The assertion on line 42 expects the parser to drop the trailing tool-call",
  "marker, but the new tokenizer keeps it. Two paths forward — patch the",
  "tokenizer's strip step, or update the expectation.",
].join(" ");

interface PlanStep {
  readonly label: string;
  readonly status: "done" | "running" | "pending";
}

const PLAN_STEPS: ReadonlyArray<PlanStep> = [
  { label: "identify the failing test", status: "done" },
  { label: "wire the regression check", status: "running" },
  { label: "rebuild dist", status: "pending" },
  { label: "publish patch", status: "pending" },
];

function StatusRow({ elapsedMs }: { elapsedMs: number }): React.ReactElement {
  const seconds = (elapsedMs / 1000).toFixed(1);
  const cost = (elapsedMs / 1000) * 0.0008;
  return (
    <inkCompat.Box flexDirection="row" gap={2}>
      <inkCompat.Text color={BRAND} bold>
        ◈ Reasonix
      </inkCompat.Text>
      <inkCompat.Text color={META}>working</inkCompat.Text>
      <inkCompat.Text color={FAINT}>{`${seconds}s elapsed`}</inkCompat.Text>
      <inkCompat.Text color={FAINT}>{`$${cost.toFixed(4)}`}</inkCompat.Text>
    </inkCompat.Box>
  );
}

function PlanCard({ frame }: { frame: number }): React.ReactElement {
  const spin = SPINNER_FRAMES[frame % SPINNER_FRAMES.length] ?? "·";
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
      {PLAN_STEPS.map((step) => {
        const { glyph, color } = decoratePlan(step.status, spin);
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

function decoratePlan(status: PlanStep["status"], spin: string): { glyph: string; color: string } {
  switch (status) {
    case "done":
      return { glyph: "✓", color: OK };
    case "running":
      return { glyph: spin, color: BRAND };
    case "pending":
      return { glyph: "○", color: PEND };
  }
}

const SHELL_WINDOW = 5;

function ShellCard({ lines, frame }: { lines: number; frame: number }): React.ReactElement {
  const total = Math.min(lines, SHELL_LINES.length);
  const startIdx = Math.max(0, total - SHELL_WINDOW);
  const visible = SHELL_LINES.slice(startIdx, total);
  const hidden = startIdx;
  const running = lines < SHELL_LINES.length;
  const spin = SPINNER_FRAMES[frame % SPINNER_FRAMES.length] ?? "·";
  return (
    <inkCompat.Box
      flexDirection="column"
      borderStyle="round"
      borderColor={BRAND}
      paddingX={1}
      marginTop={1}
    >
      <inkCompat.Box flexDirection="row" gap={1}>
        <inkCompat.Text color={BRAND}>{running ? spin : "✓"}</inkCompat.Text>
        <inkCompat.Text color={BRAND} bold>
          npm test
        </inkCompat.Text>
        {hidden > 0 ? (
          <inkCompat.Text color={FAINT}>{`(+${hidden} earlier)`}</inkCompat.Text>
        ) : null}
      </inkCompat.Box>
      {visible.map((line, i) => (
        // biome-ignore lint/suspicious/noArrayIndexKey: shell output lines are positional + append-only
        <inkCompat.Text key={`shell-${startIdx + i}`} dimColor={line.startsWith(" PASS")}>
          {line || " "}
        </inkCompat.Text>
      ))}
    </inkCompat.Box>
  );
}

function ResponseCard({
  revealed,
  frame,
}: { revealed: number; frame: number }): React.ReactElement {
  const text = RESPONSE.slice(0, revealed);
  const done = revealed >= RESPONSE.length;
  const glyph = done ? "‹" : (SPINNER_FRAMES[frame % SPINNER_FRAMES.length] ?? "·");
  return (
    <inkCompat.Box
      flexDirection="column"
      borderStyle="round"
      borderColor={OK}
      paddingX={1}
      marginTop={1}
    >
      <inkCompat.Box flexDirection="row" gap={1}>
        <inkCompat.Text color={done ? OK : BRAND}>{glyph}</inkCompat.Text>
        <inkCompat.Text>{text || "thinking…"}</inkCompat.Text>
      </inkCompat.Box>
    </inkCompat.Box>
  );
}

interface ShellProps {
  onExit: () => void;
}

export function StressShell({ onExit }: ShellProps): React.ReactElement {
  const [elapsed, setElapsed] = useState(0);
  const [planFrame, setPlanFrame] = useState(0);
  const [shellLines, setShellLines] = useState(0);
  const [shellFrame, setShellFrame] = useState(0);
  const [revealed, setRevealed] = useState(0);
  const [responseFrame, setResponseFrame] = useState(0);

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
    const id = setInterval(() => {
      setPlanFrame((f) => f + 1);
    }, 80);
    return () => clearInterval(id);
  }, []);

  useEffect(() => {
    const id = setInterval(() => {
      setShellFrame((f) => f + 1);
    }, 80);
    return () => clearInterval(id);
  }, []);

  useEffect(() => {
    const id = setInterval(() => {
      setShellLines((n) => Math.min(SHELL_LINES.length, n + 1));
    }, 250);
    return () => clearInterval(id);
  }, []);

  useEffect(() => {
    const id = setInterval(() => {
      setResponseFrame((f) => f + 1);
    }, 80);
    return () => clearInterval(id);
  }, []);

  useEffect(() => {
    const id = setInterval(() => {
      setRevealed((r) => Math.min(RESPONSE.length, r + 4));
    }, 33);
    return () => clearInterval(id);
  }, []);

  return (
    <inkCompat.Box flexDirection="column">
      <StatusRow elapsedMs={elapsed} />
      <PlanCard frame={planFrame} />
      <ShellCard lines={shellLines} frame={shellFrame} />
      <ResponseCard revealed={revealed} frame={responseFrame} />
      <inkCompat.Box marginTop={1}>
        <inkCompat.Text dimColor>stress mode · 4 concurrent live regions · Esc exit</inkCompat.Text>
      </inkCompat.Box>
    </inkCompat.Box>
  );
}

export interface StressDemoOptions {
  readonly stdout?: NodeJS.WriteStream;
  readonly stdin?: NodeJS.ReadStream;
}

export async function runStressDemo(opts: StressDemoOptions = {}): Promise<void> {
  const stdout = opts.stdout ?? process.stdout;
  const stdin = opts.stdin ?? process.stdin;

  if (!stdin.isTTY || !stdout.isTTY) {
    console.error("stress-demo requires an interactive TTY.");
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

  const handle: Handle = mount(<StressShell onExit={() => resolveExit()} />, {
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
