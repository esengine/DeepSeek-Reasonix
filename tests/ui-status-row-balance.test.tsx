/**
 * StatusRow turn-cost rendering — wallet + session-cost segments live in
 * StatsPanel / UsageCard now (covered by their own tests). This file only
 * asserts the turn-cost + cache cells StatusRow still renders.
 */
import { render } from "ink";
import React, { useEffect } from "react";
import { describe, expect, it } from "vitest";
import { SlashSuggestions } from "../src/cli/ui/SlashSuggestions.js";
import { StatusRow } from "../src/cli/ui/layout/StatusRow.js";
import type { SlashCommandSpec } from "../src/cli/ui/slash.js";
import { AgentStoreProvider, useAgentStore } from "../src/cli/ui/state/provider.js";
import type { AgentState, SessionInfo } from "../src/cli/ui/state/state.js";
import { VERSION } from "../src/version.js";
import { makeFakeStdin, makeFakeStdout } from "./helpers/ink-stdio.js";

const SESSION: SessionInfo = {
  id: "default",
  branch: "main",
  workspace: "/tmp/repo",
  model: "deepseek-chat",
};

function EventInjector({
  events,
  children,
}: {
  events: readonly unknown[];
  children: React.ReactNode;
}): React.ReactElement {
  const store = useAgentStore();
  // biome-ignore lint/correctness/useExhaustiveDependencies: mount-only dispatch
  useEffect(() => {
    for (const ev of events) store.dispatch(ev as any);
  }, []);
  return React.createElement(React.Fragment, null, children);
}

function StateInjector({
  overrides,
  children,
}: {
  overrides: Partial<AgentState["status"]>;
  children: React.ReactNode;
}): React.ReactElement {
  return React.createElement(EventInjector, {
    events: [{ type: "session.update", patch: overrides }],
    children,
  });
}

async function renderStatusRow(overrides: Partial<AgentState["status"]>): Promise<string> {
  const stdout = makeFakeStdout();
  const { unmount } = render(
    <AgentStoreProvider session={SESSION}>
      <StateInjector overrides={overrides}>
        <StatusRow />
      </StateInjector>
    </AgentStoreProvider>,
    { stdout: stdout as never, stdin: makeFakeStdin() as never },
  );
  await new Promise((r) => setTimeout(r, 50));
  unmount();
  return stdout.text();
}

describe("StatusRow — turn cost currency", () => {
  it("USD wallet: turn cost shows $", async () => {
    const text = await renderStatusRow({
      cost: 0.0308,
      balance: 0.71,
      balanceCurrency: "USD",
    } as any);
    expect(text).toContain("$0.0308 turn");
    expect(text).not.toContain(" session ");
    expect(text).not.toContain("wallet ");
  });

  it("CNY wallet: turn cost shows ¥ (USD→CNY)", async () => {
    const text = await renderStatusRow({
      cost: 0.0308,
      balance: 6.55,
      balanceCurrency: "CNY",
    } as any);
    expect(text).toContain("¥0.2218 turn");
    expect(text).not.toContain(" session ");
    expect(text).not.toContain("wallet ");
  });

  it("no wallet info: turn cost defaults to ¥", async () => {
    const text = await renderStatusRow({ cost: 0.0308, balance: undefined } as any);
    expect(text).toContain("¥0.2218 turn");
    expect(text).not.toContain("wallet ");
  });

  it("turn cost hidden when zero", async () => {
    const text = await renderStatusRow({ cost: 0 } as any);
    expect(text).not.toContain("turn");
  });

  it("cache % always rendered", async () => {
    const text = await renderStatusRow({ cost: 0, cacheHit: 0.873 } as any);
    expect(text).toContain("cache 87%");
  });
});

describe("StatusRow — statusBar config toggles", () => {
  async function renderStatusRowWithConfig(
    overrides: Partial<AgentState["status"]>,
    config: Partial<import("../src/cli/ui/layout/StatusRow.js").StatusBarConfig>,
  ): Promise<string> {
    const cfg = {
      showBalance: true,
      showSessionCost: true,
      showTurnCost: true,
      showCacheHit: true,
      showCtxUsage: true,
      showVersion: true,
      showFeedbackHint: true,
      ...config,
    };
    const stdout = makeFakeStdout();
    const { unmount } = render(
      <AgentStoreProvider session={SESSION}>
        <StateInjector overrides={overrides}>
          <StatusRow
            statusBar={cfg as import("../src/cli/ui/layout/StatusRow.js").StatusBarConfig}
          />
        </StateInjector>
      </AgentStoreProvider>,
      { stdout: stdout as never, stdin: makeFakeStdin() as never },
    );
    await new Promise((r) => setTimeout(r, 50));
    unmount();
    return stdout.text();
  }

  it("default config (all true) shows turn cost and cache", async () => {
    const text = await renderStatusRowWithConfig({ cost: 0.05, cacheHit: 0.5 } as any, {});
    expect(text).toContain("turn");
    expect(text).toContain("cache");
  });

  it("showTurnCost=false hides turn cost", async () => {
    const text = await renderStatusRowWithConfig({ cost: 0.05, cacheHit: 0.5 } as any, {
      showTurnCost: false,
    });
    expect(text).not.toContain("turn");
    expect(text).toContain("cache");
  });

  it("showCacheHit=false hides cache hit", async () => {
    const text = await renderStatusRowWithConfig({ cost: 0.05, cacheHit: 0.5 } as any, {
      showCacheHit: false,
    });
    expect(text).toContain("turn");
    expect(text).not.toContain("cache");
  });

  it("showVersion=false hides version string", async () => {
    const text = await renderStatusRowWithConfig({ cost: 0 } as any, { showVersion: false });
    expect(text).not.toContain(`v${VERSION}`);
  });

  it("showFeedbackHint=false hides /feedback hint", async () => {
    const text = await renderStatusRowWithConfig({ cost: 0 } as any, { showFeedbackHint: false });
    expect(text).not.toContain("/feedback");
  });

  it("both showBalance and showSessionCost false drops wallet pill", async () => {
    const text = await renderStatusRowWithConfig(
      { cost: 0, sessionCost: 0.01, balance: 5 } as any,
      { showBalance: false, showSessionCost: false },
    );
    expect(text).not.toContain("⛁");
  });

  it("showBalance=false still shows session cost in wallet", async () => {
    const text = await renderStatusRowWithConfig(
      { cost: 0, sessionCost: 0.01, balance: 5 } as any,
      { showBalance: false, showSessionCost: true },
    );
    expect(text).toContain("⛁");
    expect(text).toContain("spent");
    expect(text).not.toContain("left");
  });

  it("default config (all true) shows balance and session cost in wallet", async () => {
    const text = await renderStatusRowWithConfig(
      { cost: 0, sessionCost: 0.01, balance: 10, balanceCurrency: "USD" } as any,
      {},
    );
    expect(text).toContain("⛁");
    expect(text).toContain("spent");
  });

  it("ctx pill renders pct + tokens once promptTokens is known", async () => {
    const text = await renderStatusRowWithConfig(
      { cost: 0, promptTokens: 720_000, promptCap: 1_000_000 } as any,
      {},
    );
    expect(text).toContain("ctx");
    expect(text).toContain("72%");
    expect(text).toContain("720K/1000K");
  });

  it("showCtxUsage=false hides ctx pill", async () => {
    const text = await renderStatusRowWithConfig(
      { cost: 0, promptTokens: 720_000, promptCap: 1_000_000 } as any,
      { showCtxUsage: false },
    );
    expect(text).not.toContain("ctx ");
  });

  it("ctx pill hidden when promptTokens is unset", async () => {
    const text = await renderStatusRowWithConfig({ cost: 0 } as any, {});
    expect(text).not.toContain("ctx ");
  });
});

function makeSlashCommands(count: number): SlashCommandSpec[] {
  return Array.from({ length: count }, (_, i) => ({
    cmd: `cmd${i.toString().padStart(2, "0")}`,
    summary: `summary ${i}`,
    group: i < 5 ? "setup" : "info",
  }));
}

async function renderStatusWithSuggestions(): Promise<string> {
  const stdout = makeFakeStdout();
  const { unmount } = render(
    <AgentStoreProvider session={SESSION}>
      <StateInjector
        overrides={{
          mode: "auto",
          cacheHit: 0,
          balance: 8.08,
          balanceCurrency: "CNY",
        }}
      >
        <BoxLikeComposer />
      </StateInjector>
    </AgentStoreProvider>,
    { stdout: stdout as never, stdin: makeFakeStdin() as never },
  );
  await new Promise((r) => setTimeout(r, 50));
  unmount();
  return stdout.text();
}

function BoxLikeComposer(): React.ReactElement {
  return (
    <React.Fragment>
      <StatusRow />
      <SlashSuggestions matches={makeSlashCommands(12)} selectedIndex={7} groupMode />
    </React.Fragment>
  );
}

describe("StatusRow + SlashSuggestions composition", () => {
  it("keeps the status line independent from slash suggestion headers", async () => {
    const text = await renderStatusWithSuggestions();
    const lines = text.split(/\r?\n/);
    const statusLine = lines.find((line) => line.includes("cache 0%"));

    expect(statusLine).toBeDefined();
    expect(statusLine).toContain("auto");
    expect(statusLine).toContain("default · main");
    expect(statusLine).toContain(`v${VERSION}`);
    expect(statusLine).toContain("/feedback");
    expect(statusLine).not.toContain("SETUP");
    expect(statusLine).not.toContain("commands");
    expect(lines.some((line) => /^\s*SETUP\s*$/.test(line))).toBe(true);
  });
});
