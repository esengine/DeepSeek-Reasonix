// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { renderToStaticMarkup } from "react-dom/server";
import "./testkit";
import { Pane } from "./Pane";
import { Cost } from "./panels/Cost";
import { MockPort } from "../port/mock";
import type { AgentPort } from "../port/port";
import type { RuntimeView } from "../port/hub";
import { initialState } from "../state/session";
import { setHidesAmounts } from "../state/prefs";

afterEach(() => {
  cleanup();
  act(() => setHidesAmounts(false));
  localStorage.clear();
});

const rt = { id: "p1", root: "/w", name: "w" } as RuntimeView;

function open(port: MockPort) {
  return render(
    <Pane
      port={port as AgentPort}
      rt={rt}
      title="w"
      active
      visible
      sideHost={null}
      side={false}
      onFocus={() => {}}
      onReport={() => {}}
      onSessionChanged={() => {}}
      pulse={0}
      findPulse={0}
      onSettings={() => {}}
      needsProject={false}
      onOpenProject={() => {}}
      onKeepHere={() => {}}
      theme="dark"
      dockW={560}
      dockMax={880}
      onDockW={() => {}}
    />,
  );
}

const walletButton = (c: HTMLElement) => c.querySelector<HTMLElement>(".studio-meter-wallet")!;
const costFigure = (c: HTMLElement) => c.querySelector<HTMLElement>(".studio-meter-cost b")!;

describe("hiding the amounts in the meter", () => {
  // A shared screen or a recording leaks what the account holds and what the
  // session spent as readily as anything else in the bar.
  it("masks the balance and the session cost behind the eye, and keeps reading the wallet", async () => {
    const port = new MockPort();
    const balance = vi.spyOn(port, "balance");
    let pane!: ReturnType<typeof open>;
    await act(async () => { pane = open(port); });
    const { container } = pane;
    await waitFor(() => expect(walletButton(container)?.textContent).toBe("¥110.00"));
    const reads = balance.mock.calls.length;

    fireEvent.click(screen.getByRole("button", { name: "隐藏金额" }));
    expect(walletButton(container).textContent).toBe("•••");
    expect(costFigure(container).textContent).toBe("•••");
    expect(container.textContent).not.toContain("110");

    const reveal = screen.getByRole("button", { name: "显示金额" });
    expect(reveal.getAttribute("aria-pressed")).toBe("true");
    fireEvent.click(reveal);
    expect(walletButton(container).textContent).toBe("¥110.00");
    expect(costFigure(container).textContent).not.toBe("•••");
    expect(balance.mock.calls.length).toBe(reads);
  });

  it("stays hidden in the next window", async () => {
    let first!: ReturnType<typeof open>;
    await act(async () => { first = open(new MockPort()); });
    await waitFor(() => expect(walletButton(first.container)?.textContent).toBe("¥110.00"));
    fireEvent.click(screen.getByRole("button", { name: "隐藏金额" }));
    first.unmount();
    vi.resetModules();
    const prefs = await import("../state/prefs");
    expect(prefs.hidesAmounts()).toBe(true);
  });

  it("masks every amount in the cost card", () => {
    act(() => setHidesAmounts(true));
    const metrics = {
      ...initialState.metrics,
      cost: 4.37,
      turn: 0.58,
      bySource: { executor: 3.21, subagent: 1.16 },
      alt: { amount: 0.61, currency: "USD" },
    };
    const html = renderToStaticMarkup(
      <Cost
        metrics={metrics}
        wallet={{ kind: "read", reading: { display: "¥110.00", available: true, stale: false, fetchedAt: "2026-08-27T00:00:00.000Z", lines: [{ currency: "CNY", total: "¥110.00", granted: "¥10.00" }] } }}
        account="deepseek"
        onRefreshWallet={() => {}}
      />,
    );
    expect(html).toContain("钱包 · deepseek");
    expect(html).toContain("•••");
    expect(html).not.toContain("110");
    expect(html).not.toContain("¥10.00");
    for (const leak of ["4.37", "0.58", "3.21", "1.16", "0.61"]) expect(html).not.toContain(leak);
    expect(html).toContain("本回合");
    expect(html).toContain("主循环");
  });
});
