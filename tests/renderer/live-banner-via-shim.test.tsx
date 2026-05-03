// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React from "react";
import { describe, expect, it, vi } from "vitest";

vi.mock("ink", async () => {
  const compat = await import("../../src/renderer/ink-compat/index.js");
  return {
    Box: compat.Box,
    Text: compat.Text,
    Spacer: compat.Spacer,
    Static: compat.Static,
    useStdout: compat.useStdout,
    useApp: compat.useApp,
    useInput: compat.useInput,
  };
});

import { WelcomeBanner } from "../../src/cli/ui/WelcomeBanner.js";
import { CharPool, HyperlinkPool, StylePool, mount } from "../../src/renderer/index.js";
import { makeTestWriter } from "../../src/renderer/runtime/test-writer.js";

function pools() {
  return { char: new CharPool(), style: new StylePool(), hyperlink: new HyperlinkPool() };
}

function flush(): Promise<void> {
  return new Promise((r) => setTimeout(r, 0));
}

describe("live WelcomeBanner via vi.mock('ink')", () => {
  it("the actual src/cli/ui/WelcomeBanner.tsx renders end-to-end through the shim", async () => {
    const w = makeTestWriter();
    const handle = mount(<WelcomeBanner />, {
      viewportWidth: 80,
      viewportHeight: 14,
      pools: pools(),
      write: w.write,
    });
    await flush();
    const out = w.output();
    expect(out).toContain("REASONIX");
    expect(out).toContain("╔");
    expect(out).toContain("╝");
    expect(out).toContain("/help");
    expect(out).toContain("/init");
    expect(out).toContain("/memory");
    expect(out).toContain("/cost");
    handle.destroy();
  });

  it("inCodeMode swaps the tagline without breaking layout", async () => {
    const w = makeTestWriter();
    const handle = mount(<WelcomeBanner inCodeMode />, {
      viewportWidth: 80,
      viewportHeight: 14,
      pools: pools(),
      write: w.write,
    });
    await flush();
    const out = w.output();
    expect(out).toContain("REASONIX");
    expect(out).toContain("╔");
    expect(out).toContain("╚");
    handle.destroy();
  });

  it("dashboardUrl renders the web · link line", async () => {
    const w = makeTestWriter();
    const handle = mount(<WelcomeBanner dashboardUrl="http://localhost:7331" />, {
      viewportWidth: 80,
      viewportHeight: 16,
      pools: pools(),
      write: w.write,
    });
    await flush();
    const out = w.output();
    expect(out).toContain("web");
    expect(out).toContain("localhost:7331");
    handle.destroy();
  });

  it("narrow viewport (cols=40) still renders the frame", async () => {
    const w = makeTestWriter();
    const handle = mount(<WelcomeBanner />, {
      viewportWidth: 40,
      viewportHeight: 14,
      pools: pools(),
      write: w.write,
    });
    await flush();
    const out = w.output();
    expect(out).toContain("╔");
    expect(out).toContain("REASONIX");
    handle.destroy();
  });
});
