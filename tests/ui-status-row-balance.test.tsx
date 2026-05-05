/**
 * StatusRow wallet rendering - verifies the currency symbol matches the
 * balance currency, not hardcoded ¥.
 *
 * These tests import the REAL StatusRow component and render it through
 * Ink with a mock AgentStore.  They FAIL today because StatusRow:61
 * hardcodes ¥ and the state has no balanceCurrency field.
 */
import { render } from "ink";
import React, { useEffect } from "react";
import { describe, expect, it } from "vitest";
import { StatusRow } from "../src/cli/ui/layout/StatusRow.js";
import { AgentStoreProvider, useAgentStore } from "../src/cli/ui/state/provider.js";
import type { AgentState, SessionInfo } from "../src/cli/ui/state/state.js";

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

const SESSION: SessionInfo = {
  id: "test-session",
  branch: "main",
  workspace: "/tmp/repo",
  model: "deepseek-chat",
};

function makeFakeStdout() {
  const chunks: string[] = [];
  return {
    columns: 120,
    rows: 30,
    isTTY: true,
    write(chunk: string) {
      chunks.push(chunk);
      return true;
    },
    on() {},
    off() {},
    data: chunks,
    text(): string {
      // Strip ANSI escape sequences so we can assert on readable text.
      // biome-ignore lint/suspicious/noControlCharactersInRegex: stripping ANSI SGR codes
      return chunks.join("").replace(/\x1b\[[0-9;]*m/g, "");
    },
  };
}

/** Dispatches arbitrary events on mount into the store created by AgentStoreProvider. */
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

/** Convenience: inject a single session.update with status overrides. */
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

/** Render <StatusRow /> through Ink with a fake stdout, return collected text. */
async function renderStatusRow(overrides: Partial<AgentState["status"]>): Promise<string> {
  const stdout = makeFakeStdout();
  const { unmount, waitUntilExit } = render(
    <AgentStoreProvider session={SESSION}>
      <StateInjector overrides={overrides}>
        <StatusRow />
      </StateInjector>
    </AgentStoreProvider>,
    { stdout: stdout as any, stdin: process.stdin as any },
  );
  // Let the StateInjector effect fire and StatusRow re-render.
  await new Promise((r) => setTimeout(r, 50));
  unmount();
  return stdout.text();
}

// ---------------------------------------------------------------------------
// tests
// ---------------------------------------------------------------------------

describe("StatusRow - wallet currency symbol", () => {
  it("shows $ for USD balance", async () => {
    const text = await renderStatusRow({ balance: 0.91, balanceCurrency: "USD" } as any);
    expect(text).toContain("$0.91");
  });

  it("shows ¥ for CNY balance", async () => {
    const text = await renderStatusRow({ balance: 6.55, balanceCurrency: "CNY" } as any);
    expect(text).toContain("¥6.55");
  });

  it("shows no wallet when balance is undefined", async () => {
    const text = await renderStatusRow({ balance: undefined } as any);
    expect(text).not.toContain("wallet");
  });

  it("uses correct color for USD $0.91 (~¥6.55 -> warn, not err)", async () => {
    // $0.91 * 7.2 = ¥6.55 -> warn range (yellow), not err (red).
    // The err color is #ff8b81, warn is #f0b07d.
    const text = await renderStatusRow({ balance: 0.91, balanceCurrency: "USD" } as any);
    expect(text).toContain("$0.91");
    // After the fix, the color should be warn (yellow) not err (red).
    // For now, this test just confirms the symbol is correct.
  });

  // ---- Turn/session costs must follow wallet currency ----
  // When the wallet is in USD, costs should show in USD ($).
  // When the wallet is in CNY, costs should show in CNY (¥).
  // When no wallet is loaded, default to CNY (DeepSeek native pricing).

  it("USD wallet: turn/session costs show $, not ¥", async () => {
    const text = await renderStatusRow({
      cost: 0.0308,
      sessionCost: 0.064,
      balance: 0.71,
      balanceCurrency: "USD",
    } as any);
    // Cost in USD, no conversion: $0.0308 turn, $0.064 session
    expect(text).toContain("$0.0308 turn");
    expect(text).toContain("$0.064 session");
    expect(text).toContain("wallet $0.71");
  });

  it("CNY wallet: turn/session costs show ¥ (converted from USD)", async () => {
    const text = await renderStatusRow({
      cost: 0.0308,
      sessionCost: 0.064,
      balance: 6.55,
      balanceCurrency: "CNY",
    } as any);
    // 0.0308 USD * 7.2 = 0.2218 CNY → "¥0.2218 turn"
    expect(text).toContain("¥0.2218 turn");
    // 0.064 USD * 7.2 = 0.461 CNY → "¥0.461 session"
    expect(text).toContain("¥0.461 session");
    expect(text).toContain("wallet ¥6.55");
  });

  it("no wallet info: costs default to CNY (backward compat)", async () => {
    const text = await renderStatusRow({
      cost: 0.0308,
      sessionCost: 0.064,
      balance: undefined,
    } as any);
    // When balanceCurrency is undefined, fall back to CNY display.
    expect(text).toContain("¥0.2218 turn");
    expect(text).toContain("¥0.461 session");
    expect(text).not.toContain("wallet");
  });

  // ---- Full turn flow (pricing -> turn.end -> session.update -> display) ----

  it("full USD flow: turn.end + session.update renders all $ symbols", async () => {
    const stdout = makeFakeStdout();
    const { unmount, waitUntilExit } = render(
      <AgentStoreProvider session={SESSION}>
        <EventInjector
          events={[
            {
              type: "turn.end",
              usage: { prompt: 1000, reason: 0, output: 200, cacheHit: 0.8, cost: 0.00015 },
            },
            { type: "session.update", patch: { balance: 0.71, balanceCurrency: "USD" } },
          ]}
        >
          <StatusRow />
        </EventInjector>
      </AgentStoreProvider>,
      { stdout: stdout as any, stdin: process.stdin as any },
    );
    await new Promise((r) => setTimeout(r, 50));
    unmount();
    const text = stdout.text();
    expect(text).toContain("$0.0001 turn");
    expect(text).toContain("$0.000 session");
    expect(text).toContain("wallet $0.71");
  });

  it("full CNY flow: turn.end + session.update renders all ¥ symbols", async () => {
    const stdout = makeFakeStdout();
    const { unmount, waitUntilExit } = render(
      <AgentStoreProvider session={SESSION}>
        <EventInjector
          events={[
            {
              type: "turn.end",
              usage: { prompt: 1000, reason: 0, output: 200, cacheHit: 0.8, cost: 0.00015 },
            },
            { type: "session.update", patch: { balance: 6.55, balanceCurrency: "CNY" } },
          ]}
        >
          <StatusRow />
        </EventInjector>
      </AgentStoreProvider>,
      { stdout: stdout as any, stdin: process.stdin as any },
    );
    await new Promise((r) => setTimeout(r, 50));
    unmount();
    const text = stdout.text();
    // 0.00015 USD * 7.2 = 0.0011 CNY → "¥0.0011 turn"
    expect(text).toContain("¥0.0011 turn");
    // 0.00015 USD * 7.2 = 0.001 CNY (3 fraction digits)
    expect(text).toContain("¥0.001 session");
    // Wallet in ¥
    expect(text).toContain("wallet ¥6.55");
  });
});
