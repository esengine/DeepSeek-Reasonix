// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React, { useState } from "react";
import {
  Box,
  CharPool,
  HyperlinkPool,
  type Keystroke,
  StylePool,
  Text,
  mount,
  useKeystroke,
} from "../../renderer/index.js";

const ACCENT = { apply: "\x1b[36m", revert: "\x1b[39m" };
const DIM = { apply: "\x1b[2m", revert: "\x1b[22m" };
const FAINT_BORDER = [{ apply: "\x1b[90m", revert: "\x1b[39m" }];

function describeKey(k: Keystroke): string {
  if (k.escape) return "ESC";
  if (k.return) return "Return";
  if (k.tab) return "Tab";
  if (k.backspace) return "Backspace";
  if (k.delete) return "Delete";
  if (k.upArrow) return `${modPrefix(k)}↑`;
  if (k.downArrow) return `${modPrefix(k)}↓`;
  if (k.leftArrow) return `${modPrefix(k)}←`;
  if (k.rightArrow) return `${modPrefix(k)}→`;
  if (k.home) return "Home";
  if (k.end) return "End";
  if (k.pageUp) return "PageUp";
  if (k.pageDown) return "PageDown";
  if (k.ctrl && k.input) return `Ctrl+${k.input.toUpperCase()}`;
  if (k.meta && k.input) return `Alt+${k.input}`;
  return k.input || "?";
}

function modPrefix(k: Keystroke): string {
  let p = "";
  if (k.ctrl) p += "Ctrl+";
  if (k.meta) p += "Alt+";
  if (k.shift) p += "Shift+";
  return p;
}

function Demo({ onExit }: { onExit: () => void }) {
  const [count, setCount] = useState(0);
  const [last, setLast] = useState<string>("(none yet)");

  useKeystroke((k) => {
    if (k.escape) {
      onExit();
      return;
    }
    setCount((n) => n + 1);
    setLast(describeKey(k));
  });

  return (
    <Box flexDirection="column" borderStyle="round" borderColor={FAINT_BORDER} padding={1}>
      <Text style={[ACCENT]}>Reasonix cell-diff renderer · demo</Text>
      <Text style={[DIM]}>Press any key. ESC to exit.</Text>
      <Box paddingTop={1} flexDirection="row">
        <Text>{"Count : "}</Text>
        <Text>{String(count)}</Text>
      </Box>
      <Box flexDirection="row">
        <Text>{"Last  : "}</Text>
        <Text>{last}</Text>
      </Box>
    </Box>
  );
}

export interface RendererDemoOptions {
  /** Optional override for stdout (used by tests). */
  stdout?: NodeJS.WriteStream;
  /** Optional override for stdin (used by tests). */
  stdin?: NodeJS.ReadStream;
}

export async function runRendererDemo(opts: RendererDemoOptions = {}): Promise<void> {
  const stdout = opts.stdout ?? process.stdout;
  const stdin = opts.stdin ?? process.stdin;

  if (!stdin.isTTY || !stdout.isTTY) {
    console.error("renderer-demo requires an interactive TTY.");
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

  const handle = mount(<Demo onExit={() => resolveExit()} />, {
    viewportWidth: stdout.columns ?? 80,
    viewportHeight: stdout.rows ?? 24,
    pools,
    write: (bytes) => stdout.write(bytes),
    stdin,
  });

  const onResize = () => {
    handle.resize(stdout.columns ?? 80, stdout.rows ?? 24);
  };
  stdout.on("resize", onResize);

  try {
    await exited;
  } finally {
    stdout.off("resize", onResize);
    handle.destroy();
    stdin.pause();
  }
}
