// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React, { useRef, useState } from "react";
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
const ACCENT = "#d2a8ff";
const OK = "#7ee787";
const ERR = "#ff8b81";
const META = "#8b949e";

interface HistoryEntry {
  readonly id: number;
  readonly role: "user" | "echo" | "info" | "error";
  readonly text: string;
}

interface SlashCommand {
  readonly name: string;
  readonly summary: string;
}

const SLASH_COMMANDS: ReadonlyArray<SlashCommand> = [
  { name: "/help", summary: "list available commands" },
  { name: "/clear", summary: "clear the history" },
  { name: "/exit", summary: "leave the preview" },
];

function Header(): React.ReactElement {
  return (
    <inkCompat.Box flexDirection="row" gap={1}>
      <inkCompat.Text color={BRAND} bold>
        ◈ Reasonix
      </inkCompat.Text>
      <inkCompat.Text color={FAINT}>preview · cell-diff renderer</inkCompat.Text>
    </inkCompat.Box>
  );
}

function HistoryRow({ entry }: { entry: HistoryEntry }): React.ReactElement {
  const { tone, glyph } = decorate(entry.role);
  return (
    <inkCompat.Box flexDirection="row" gap={1}>
      <inkCompat.Text color={tone}>{glyph}</inkCompat.Text>
      <inkCompat.Text color={entry.role === "info" ? META : undefined}>{entry.text}</inkCompat.Text>
    </inkCompat.Box>
  );
}

function decorate(role: HistoryEntry["role"]): { tone: string; glyph: string } {
  switch (role) {
    case "user":
      return { tone: ACCENT, glyph: "›" };
    case "echo":
      return { tone: OK, glyph: "‹" };
    case "info":
      return { tone: META, glyph: "·" };
    case "error":
      return { tone: ERR, glyph: "✖" };
  }
}

interface PromptLineProps {
  readonly value: string;
  readonly placeholder: string;
}

function PromptLine({ value, placeholder }: PromptLineProps): React.ReactElement {
  const showPlaceholder = value.length === 0;
  return (
    <inkCompat.Box flexDirection="row" gap={1} marginTop={1}>
      <inkCompat.Text color={BRAND} bold>
        ›
      </inkCompat.Text>
      {showPlaceholder ? (
        <inkCompat.Text dimColor>{placeholder}</inkCompat.Text>
      ) : (
        <inkCompat.Text>{value}</inkCompat.Text>
      )}
      <inkCompat.Text color={FAINT}>▏</inkCompat.Text>
    </inkCompat.Box>
  );
}

function HintBar(): React.ReactElement {
  return (
    <inkCompat.Box marginTop={1}>
      <inkCompat.Text dimColor>Enter submit · / for commands · Esc exit</inkCompat.Text>
    </inkCompat.Box>
  );
}

interface ShellProps {
  onExit: () => void;
}

interface Reply {
  readonly role: "echo" | "info" | "error";
  readonly text: string;
}

function handleSubmit(
  text: string,
  onExit: () => void,
  onClear: () => void,
): ReadonlyArray<Reply> | "cleared" {
  if (!text.startsWith("/")) {
    return [{ role: "echo", text: `you said: ${text}` }];
  }
  const [name] = text.split(/\s+/, 1);
  const cmd = name ?? text;
  if (cmd === "/exit") {
    onExit();
    return [{ role: "info", text: "exiting…" }];
  }
  if (cmd === "/clear") {
    onClear();
    return "cleared";
  }
  if (cmd === "/help") {
    return [
      { role: "info", text: "available commands:" },
      ...SLASH_COMMANDS.map((c) => ({ role: "info" as const, text: `  ${c.name} — ${c.summary}` })),
    ];
  }
  return [{ role: "error", text: `unknown command: ${cmd}` }];
}

export function PreviewShell({ onExit }: ShellProps): React.ReactElement {
  const [history, setHistory] = useState<ReadonlyArray<HistoryEntry>>([]);
  const [draft, setDraft] = useState("");
  const draftRef = useRef("");
  const nextIdRef = useRef(0);

  useKeystroke((k) => {
    if (k.escape) {
      onExit();
      return;
    }
    if (k.return) {
      const text = draftRef.current.trim();
      if (text.length === 0) return;
      const replies = handleSubmit(text, onExit, () => {
        nextIdRef.current = 0;
        setHistory([]);
      });
      if (replies !== "cleared") {
        const id = nextIdRef.current;
        nextIdRef.current = id + 1 + replies.length;
        setHistory((prev) => [
          ...prev,
          { id, role: "user", text },
          ...replies.map((r, i) => ({ id: id + 1 + i, role: r.role, text: r.text })),
        ]);
      }
      draftRef.current = "";
      setDraft("");
      return;
    }
    if (k.backspace) {
      const next = draftRef.current.slice(0, -1);
      draftRef.current = next;
      setDraft(next);
      return;
    }
    if (k.ctrl || k.meta) return;
    if (k.input && k.input.length > 0) {
      const next = draftRef.current + k.input;
      draftRef.current = next;
      setDraft(next);
    }
  });

  return (
    <inkCompat.Box flexDirection="column">
      <Header />
      <inkCompat.Box flexDirection="column" marginTop={1}>
        <inkCompat.Static items={history}>
          {(entry) => <HistoryRow key={entry.id} entry={entry} />}
        </inkCompat.Static>
      </inkCompat.Box>
      <PromptLine value={draft} placeholder="say something…" />
      <HintBar />
    </inkCompat.Box>
  );
}

export interface PreviewOptions {
  readonly stdout?: NodeJS.WriteStream;
  readonly stdin?: NodeJS.ReadStream;
}

export async function runPreview(opts: PreviewOptions = {}): Promise<void> {
  const stdout = opts.stdout ?? process.stdout;
  const stdin = opts.stdin ?? process.stdin;

  if (!stdin.isTTY || !stdout.isTTY) {
    console.error("preview requires an interactive TTY.");
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

  const handle: Handle = mount(<PreviewShell onExit={() => resolveExit()} />, {
    viewportWidth: stdout.columns ?? 80,
    viewportHeight: stdout.rows ?? 24,
    pools,
    write: (bytes) => stdout.write(bytes),
    stdin,
    onExit: () => resolveExit(),
  });

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
