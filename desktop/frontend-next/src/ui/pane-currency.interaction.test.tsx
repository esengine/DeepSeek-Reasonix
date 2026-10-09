// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, render, waitFor } from "@testing-library/react";
import "./testkit";
import { Pane } from "./Pane";
import { MockPort } from "../port/mock";
import type { AgentPort } from "../port/port";
import type { RuntimeView } from "../port/hub";
import type { CostQuote, WireEvent } from "../port/wire";

afterEach(cleanup);

const rt = { id: "p1", root: "/w", name: "w" } as RuntimeView;

const quote = (amount: string, currency: string) =>
  ({ original: { amount, currency }, selected: { amount, currency }, coverage: "complete" }) as unknown as CostQuote;

describe("the session cost chip when the display currency changes", () => {
  it("re-reads the session total and the wallet in the new currency without a turn", async () => {
    const port = new MockPort();
    let current = quote("1.50", "CNY");
    const status = port.status.bind(port);
    vi.spyOn(port, "status").mockImplementation(async () => ({ ...(await status()), sessionCostQuote: current }));
    let emit: (ev: WireEvent) => void = () => {};
    const subscribe = port.subscribe.bind(port);
    vi.spyOn(port, "subscribe").mockImplementation((onEvent, onGap, bootstrap) => {
      emit = onEvent;
      return subscribe(onEvent, onGap, bootstrap);
    });
    const balance = vi.spyOn(port, "balance");
    const { container } = render(
      <Pane port={port as AgentPort} rt={rt} title="w" active visible sideHost={null} side={false}
        onFocus={() => {}} onReport={() => {}} onSessionChanged={() => {}} pulse={0} findPulse={0}
        onSettings={() => {}} needsProject={false} onOpenProject={() => {}} onKeepHere={() => {}}
        theme="dark" dockW={560} dockMax={880} onDockW={() => {}} />,
    );
    const figure = () => container.querySelector<HTMLElement>(".studio-meter-cost b")?.textContent;
    await waitFor(() => expect(figure()).toBe("¥1.50"));
    const reads = balance.mock.calls.length;

    current = quote("0.21", "USD");
    act(() => emit({ kind: "notice", level: "info", text: "currency set", code: "display_currency", detail: "USD" } as WireEvent));
    await waitFor(() => expect(figure()).toMatch(/^US?\$0\.21$/));
    expect(balance.mock.calls.length).toBeGreaterThan(reads);
  });
});
