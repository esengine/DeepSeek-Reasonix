// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React from "react";
import { describe, expect, it } from "vitest";
import {
  CharPool,
  HyperlinkPool,
  type KeystrokeSource,
  StylePool,
  inkCompat,
  mount,
  useKeystroke,
} from "../../src/renderer/index.js";
import { makeTestWriter } from "../../src/renderer/runtime/test-writer.js";

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

function pools() {
  return { char: new CharPool(), style: new StylePool(), hyperlink: new HyperlinkPool() };
}

function flush(): Promise<void> {
  return new Promise((r) => setTimeout(r, 0));
}

function makeFakeStdin(): KeystrokeSource & { push: (s: string) => void } {
  let listener: ((c: string | Buffer) => void) | null = null;
  return {
    on(_e, cb) {
      listener = cb;
    },
    off() {
      listener = null;
    },
    setRawMode() {},
    resume() {},
    pause() {},
    push(c: string) {
      listener?.(c);
    },
  };
}

describe("banner-demo — end-to-end via ink-compat", () => {
  it("renders frame corners, brand line, hint row", async () => {
    const w = makeTestWriter();
    const handle = mount(<Banner onExit={() => {}} />, {
      viewportWidth: 60,
      viewportHeight: 14,
      pools: pools(),
      write: w.write,
      stdin: makeFakeStdin(),
    });
    await flush();
    const out = w.output();
    expect(out).toContain("╔");
    expect(out).toContain("╗");
    expect(out).toContain("╚");
    expect(out).toContain("╝");
    expect(out).toContain("REASONIX");
    expect(out).toContain("/help");
    expect(out).toContain("/cost");
    handle.destroy();
  });

  it("ESC triggers onExit", async () => {
    const w = makeTestWriter();
    const stdin = makeFakeStdin();
    let exited = false;
    const handle = mount(
      <Banner
        onExit={() => {
          exited = true;
        }}
      />,
      {
        viewportWidth: 60,
        viewportHeight: 14,
        pools: pools(),
        write: w.write,
        stdin,
      },
    );
    await flush();
    stdin.push("\x1b");
    await flush();
    expect(exited).toBe(true);
    handle.destroy();
  });

  it("resize re-paints with the new viewport width", async () => {
    const w = makeTestWriter();
    const handle = mount(<Banner onExit={() => {}} />, {
      viewportWidth: 60,
      viewportHeight: 14,
      pools: pools(),
      write: w.write,
      stdin: makeFakeStdin(),
    });
    await flush();
    w.flush();
    handle.resize(100, 14);
    await flush();
    expect(w.output()).toContain("╔");
    handle.destroy();
  });
});
