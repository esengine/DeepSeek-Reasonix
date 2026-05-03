// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React from "react";
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
const BOX_INNER_WIDTH = 35;

const HINTS = ["/help", "/init", "/memory", "/cost"] as const;

function centerInside(text: string, pad: number): string {
  if (text.length >= pad) return text;
  const left = Math.floor((pad - text.length) / 2);
  const right = pad - text.length - left;
  return `${" ".repeat(left)}${text}${" ".repeat(right)}`;
}

function Banner({ onExit }: { onExit: () => void }): React.ReactElement {
  const { stdout } = inkCompat.useStdout();
  const cols = stdout.columns;
  const boxWidth = BOX_INNER_WIDTH + 2;
  const boxIndent = Math.max(2, Math.floor((cols - boxWidth) / 2));
  const pad = " ".repeat(boxIndent);
  const empty = `║${" ".repeat(BOX_INNER_WIDTH)}║`;
  const row = (s: string) => `${pad}${s}`;

  useKeystroke((k) => {
    if (k.escape) onExit();
  });

  return (
    <inkCompat.Box flexDirection="column" marginY={1}>
      <inkCompat.Text color={BRAND}>{row(`╔${"═".repeat(BOX_INNER_WIDTH)}╗`)}</inkCompat.Text>
      <inkCompat.Text color={BRAND}>{row(empty)}</inkCompat.Text>
      <inkCompat.Text color={BRAND} bold>
        {row(`║${centerInside("◈  REASONIX", BOX_INNER_WIDTH)}║`)}
      </inkCompat.Text>
      <inkCompat.Text color={BRAND}>{row(empty)}</inkCompat.Text>
      <inkCompat.Text color={BRAND}>
        {row(`║${centerInside("cell-diff renderer demo", BOX_INNER_WIDTH)}║`)}
      </inkCompat.Text>
      <inkCompat.Text color={BRAND}>
        {row(`║${centerInside("press ESC to exit", BOX_INNER_WIDTH)}║`)}
      </inkCompat.Text>
      <inkCompat.Text color={BRAND}>{row(empty)}</inkCompat.Text>
      <inkCompat.Text color={BRAND}>{row(`╚${"═".repeat(BOX_INNER_WIDTH)}╝`)}</inkCompat.Text>

      <inkCompat.Box marginTop={1} flexDirection="row" justifyContent="center">
        {HINTS.map((cmd, i) => (
          <React.Fragment key={cmd}>
            <inkCompat.Text color={FAINT}>{cmd}</inkCompat.Text>
            {i < HINTS.length - 1 ? (
              <inkCompat.Text color={ACCENT}>{"   ·   "}</inkCompat.Text>
            ) : null}
          </React.Fragment>
        ))}
      </inkCompat.Box>
    </inkCompat.Box>
  );
}

export interface BannerDemoOptions {
  readonly stdout?: NodeJS.WriteStream;
  readonly stdin?: NodeJS.ReadStream;
}

export async function runBannerDemo(opts: BannerDemoOptions = {}): Promise<void> {
  const stdout = opts.stdout ?? process.stdout;
  const stdin = opts.stdin ?? process.stdin;

  if (!stdin.isTTY || !stdout.isTTY) {
    console.error("banner-demo requires an interactive TTY.");
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

  const handle: Handle = mount(<Banner onExit={() => resolveExit()} />, {
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
