// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React from "react";
import { describe, expect, it } from "vitest";
import { CardLayout } from "../../src/cli/ui/cards/CardLayout.js";
import { ProgressBar } from "../../src/cli/ui/cards/ProgressBar.js";
import { CharPool, HyperlinkPool, StylePool, mount } from "../../src/renderer/index.js";
import { Box, Text } from "../../src/renderer/ink-compat/index.js";
import { makeTestWriter } from "../../src/renderer/runtime/test-writer.js";

function pools() {
  return { char: new CharPool(), style: new StylePool(), hyperlink: new HyperlinkPool() };
}

function flush(): Promise<void> {
  return new Promise((r) => setTimeout(r, 0));
}

async function render(node: React.ReactElement, height = 8): Promise<string> {
  const w = makeTestWriter();
  const handle = mount(node, {
    viewportWidth: 80,
    viewportHeight: height,
    pools: pools(),
    write: w.write,
  });
  await flush();
  const out = w.output();
  handle.destroy();
  return out;
}

describe("CardLayout", () => {
  it("renders glyph + bold title", async () => {
    const out = await render(<CardLayout glyph="◆" tone="#79c0ff" title="reasoning" />);
    expect(out).toContain("◆");
    expect(out).toContain("reasoning");
  });

  it("string meta gets a leading `·` and faint color", async () => {
    const out = await render(<CardLayout glyph="▶" tone="#79c0ff" title="step 1" meta="2.1s" />);
    expect(out).toMatch(/2\.1s/);
    expect(out).toMatch(/·/);
  });

  it("body is indented under the header", async () => {
    const out = await render(
      <CardLayout glyph="‹" tone="#7ee787" title="reply">
        <Text>indented body</Text>
      </CardLayout>,
    );
    expect(out).toContain("indented body");
    expect(out).toContain("reply");
  });

  it("trailing slot renders inline at the right of the header", async () => {
    const out = await render(
      <CardLayout glyph="▢" tone="#79c0ff" title="shell" trailing={<Text>WORKING</Text>} />,
    );
    expect(out).toContain("WORKING");
  });
});

describe("ProgressBar", () => {
  it("draws a 0% bar as all empty", async () => {
    const out = await render(
      <Box>
        <ProgressBar ratio={0} color="#79c0ff" cells={10} />
      </Box>,
    );
    // 10 empty cells; no filled blocks should be present.
    expect(out).toContain("░");
    expect(out).not.toContain("█");
  });

  it("draws a 100% bar as all filled", async () => {
    const out = await render(
      <Box>
        <ProgressBar ratio={1} color="#79c0ff" cells={10} />
      </Box>,
    );
    expect(out).toContain("█");
    expect(out).not.toContain("░");
  });

  it("draws a half bar as half filled / half empty", async () => {
    const out = await render(
      <Box>
        <ProgressBar ratio={0.5} color="#79c0ff" cells={10} />
      </Box>,
    );
    expect(out).toContain("█");
    expect(out).toContain("░");
  });

  it("clamps out-of-range ratios", async () => {
    const negative = await render(
      <Box>
        <ProgressBar ratio={-0.5} color="#79c0ff" cells={4} />
      </Box>,
    );
    const huge = await render(
      <Box>
        <ProgressBar ratio={2} color="#79c0ff" cells={4} />
      </Box>,
    );
    expect(negative).not.toContain("█");
    expect(huge).not.toContain("░");
  });
});
