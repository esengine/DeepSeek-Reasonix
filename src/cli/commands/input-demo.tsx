// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React, { useState } from "react";
import {
  CharPool,
  type Handle,
  HyperlinkPool,
  StylePool,
  inkCompat,
  mount,
} from "../../renderer/index.js";
import { SimplePromptInput } from "../ui/prompt-input-v2.js";

const BRAND = "#79c0ff";
const FAINT = "#6e7681";
const ACCENT = "#d2a8ff";
const OK = "#7ee787";

interface Submission {
  readonly id: number;
  readonly text: string;
}

function Header(): React.ReactElement {
  return (
    <inkCompat.Box flexDirection="row" gap={1}>
      <inkCompat.Text color={BRAND} bold>
        ◈ Reasonix
      </inkCompat.Text>
      <inkCompat.Text color={FAINT}>
        input-demo · cell-diff renderer · Esc Esc to exit
      </inkCompat.Text>
    </inkCompat.Box>
  );
}

function HistoryRow({ entry }: { entry: Submission }): React.ReactElement {
  return (
    <inkCompat.Box flexDirection="row" gap={1}>
      <inkCompat.Text color={ACCENT}>›</inkCompat.Text>
      <inkCompat.Text color={OK}>#{entry.id}</inkCompat.Text>
      <inkCompat.Text>{entry.text}</inkCompat.Text>
    </inkCompat.Box>
  );
}

interface ShellProps {
  readonly onExit: () => void;
}

export function InputDemoShell({ onExit }: ShellProps): React.ReactElement {
  const [history, setHistory] = useState<ReadonlyArray<Submission>>([]);
  const [draft, setDraft] = useState("");
  const nextIdRef = React.useRef(1);

  return (
    <inkCompat.Box flexDirection="column">
      <Header />
      <inkCompat.Box flexDirection="column" marginTop={1}>
        <inkCompat.Static items={history}>
          {(entry) => <HistoryRow key={entry.id} entry={entry} />}
        </inkCompat.Static>
      </inkCompat.Box>
      <inkCompat.Box marginTop={1}>
        <SimplePromptInput
          value={draft}
          onChange={setDraft}
          onSubmit={(value) => {
            const trimmed = value.trim();
            if (trimmed.length === 0) return;
            const id = nextIdRef.current;
            nextIdRef.current = id + 1;
            setHistory((prev) => [...prev, { id, text: trimmed }]);
            setDraft("");
          }}
          onCancel={onExit}
          placeholder="type a message and hit enter…"
        />
      </inkCompat.Box>
      <inkCompat.Box marginTop={1}>
        <inkCompat.Text dimColor>
          Enter submit · Esc clear / exit when empty · ←→ move · ⌘a/⌘e jump · ⌘u/⌘k kill
        </inkCompat.Text>
      </inkCompat.Box>
    </inkCompat.Box>
  );
}

export interface InputDemoOptions {
  readonly stdout?: NodeJS.WriteStream;
  readonly stdin?: NodeJS.ReadStream;
}

export async function runInputDemo(opts: InputDemoOptions = {}): Promise<void> {
  const stdout = opts.stdout ?? process.stdout;
  const stdin = opts.stdin ?? process.stdin;

  if (!stdin.isTTY || !stdout.isTTY) {
    process.stderr.write("input-demo requires an interactive TTY.\n");
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

  stdout.write("\x1b[?2004h");

  const handle: Handle = mount(<InputDemoShell onExit={() => resolveExit()} />, {
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
    stdout.write("\x1b[?2004l");
    stdin.pause();
  }
}
