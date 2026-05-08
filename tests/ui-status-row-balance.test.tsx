/**
 * StatusRow turn-cost rendering — wallet + session-cost segments live in
 * StatsPanel / UsageCard now (covered by their own tests). This file only
 * asserts the turn-cost + cache cells StatusRow still renders.
 */
import { render } from "ink";
import React, { useEffect } from "react";
import { describe, expect, it } from "vitest";
import { StatusRow } from "../src/cli/ui/layout/StatusRow.js";
import { AgentStoreProvider, useAgentStore } from "../src/cli/ui/state/provider.js";
import type { AgentState, SessionInfo } from "../src/cli/ui/state/state.js";
import { makeFakeStdin, makeFakeStdout } from "./helpers/ink-stdio.js";

const SESSION: SessionInfo = {
  id: "test-session",
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
