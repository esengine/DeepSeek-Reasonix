// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, render, waitFor } from "@testing-library/react";
import "./testkit";
import { Pane } from "./Pane";
import { MockPort } from "../port/mock";
import type { AgentPort, WalletReading } from "../port/port";
import type { RuntimeView } from "../port/hub";
import type { WireEvent } from "../port/wire";

afterEach(cleanup);

const rt = { id: "p1", root: "/w", name: "w" } as RuntimeView;
const reading = (display: string, available = true): WalletReading => ({ display, available, stale: false, fetchedAt: new Date().toISOString() });

const tree = (port: MockPort, pulse: number) => (
    <Pane port={port as AgentPort} rt={rt} title="w" active visible sideHost={null} side={false}
      onFocus={() => {}} onReport={() => {}} onSessionChanged={() => {}} pulse={pulse} findPulse={0}
      onSettings={() => {}} needsProject={false} onOpenProject={() => {}} onKeepHere={() => {}}
      theme="dark" dockW={560} dockMax={880} onDockW={() => {}} />
);

function mount(port: MockPort) {
  const view = render(tree(port, 0));
  let n = 0;
  const switchTo = async (ref: string) => {
    await port.setModel(ref);
    await act(async () => { view.rerender(tree(port, ++n)); });
  };
  return Object.assign(view, { switchTo });
}

const figure = (c: HTMLElement) => c.querySelector<HTMLElement>(".studio-meter-wallet b")?.textContent ?? null;

function wallets(port: MockPort, by: Record<string, () => Promise<WalletReading | null>>) {
  return vi.spyOn(port, "balance").mockImplementation(() => by[(port as unknown as { state: { modelRef: string } }).state.modelRef]!());
}

describe("the wallet follows the model's source", () => {
  it("re-reads on a model switch and never shows the old source's number on the new one", async () => {
    const port = new MockPort();
    let release: (r: WalletReading) => void = () => {};
    const balance = wallets(port, {
      "deepseek/deepseek-v4-pro": async () => reading("¥110.00"),
      "other/m": () => new Promise((res) => { release = res; }),
    });
    const { container, switchTo } = mount(port);
    await waitFor(() => expect(figure(container)).toBe("¥110.00"));
    const reads = balance.mock.calls.length;
    await switchTo("other/m");
    await waitFor(() => expect(balance.mock.calls.length).toBeGreaterThan(reads));
    expect(figure(container)).toBeNull();
    await act(async () => release(reading("$9.00")));
    await waitFor(() => expect(figure(container)).toBe("$9.00"));
  });

  it("drops the old wallet when the new source has none", async () => {
    const port = new MockPort();
    wallets(port, { "deepseek/deepseek-v4-pro": async () => reading("¥110.00"), "other/m": async () => null });
    const { container, switchTo } = mount(port);
    await waitFor(() => expect(figure(container)).toBe("¥110.00"));
    await switchTo("other/m");
    await waitFor(() => expect(figure(container)).toBeNull());
  });

  it("drops the old wallet when the new source's read fails", async () => {
    const port = new MockPort();
    wallets(port, { "deepseek/deepseek-v4-pro": async () => reading("¥110.00"), "other/m": async () => { throw new Error("down"); } });
    const { container, switchTo } = mount(port);
    await waitFor(() => expect(figure(container)).toBe("¥110.00"));
    await switchTo("other/m");
    await waitFor(() => expect(figure(container)).toBeNull());
  });

  it("keeps the wallet and does not re-read when the model changes within one account", async () => {
    const port = new MockPort();
    const balance = vi.spyOn(port, "balance");
    const { container, switchTo } = mount(port);
    await waitFor(() => expect(figure(container)).toBe("¥110.00"));
    const reads = balance.mock.calls.length;
    await switchTo("deepseek/deepseek-flash");
    await waitFor(() => expect(port.status().then((s) => s.modelRef)).resolves.toBe("deepseek/deepseek-flash"));
    expect(figure(container)).toBe("¥110.00");
    expect(balance.mock.calls.length).toBe(reads);
  });

  it("keeps the wallet and does not re-read when the first status names the account a read already answered", async () => {
    const port = new MockPort();
    const status = port.status.bind(port);
    let release: () => void = () => {};
    const gate = new Promise<void>((res) => { release = res; });
    const statusCalls = vi.spyOn(port, "status").mockImplementation(() => gate.then(status));
    const balance = vi.spyOn(port, "balance");
    const { container } = mount(port);
    await waitFor(() => expect(figure(container)).toBe("¥110.00"));
    const reads = balance.mock.calls.length;
    const shown: Array<string | null> = [];
    const watch = new MutationObserver(() => shown.push(figure(container)));
    watch.observe(container, { childList: true, subtree: true, characterData: true });
    await act(async () => { release(); await Promise.all(statusCalls.mock.results.map((r) => r.value)); });
    watch.disconnect();
    expect(shown).not.toContain(null);
    expect(figure(container)).toBe("¥110.00");
    expect(balance.mock.calls.length).toBe(reads);
  });

  it("keeps only the last of two quick switches", async () => {
    const port = new MockPort();
    const pending: Record<string, (r: WalletReading) => void> = {};
    wallets(port, {
      "deepseek/deepseek-v4-pro": async () => reading("¥110.00"),
      "a/m": () => new Promise((res) => { pending.a = res; }),
      "b/m": () => new Promise((res) => { pending.b = res; }),
    });
    const { container, switchTo } = mount(port);
    await waitFor(() => expect(figure(container)).toBe("¥110.00"));
    await switchTo("a/m");
    await waitFor(() => expect(pending.a).toBeTruthy());
    await switchTo("b/m");
    await waitFor(() => expect(pending.b).toBeTruthy());
    await act(async () => pending.b!(reading("B 2.00")));
    await act(async () => pending.a!(reading("A 1.00")));
    expect(figure(container)).toBe("B 2.00");
  });

  it("still reads on mount and when the display currency changes", async () => {
    const port = new MockPort();
    let emit: (ev: WireEvent) => void = () => {};
    const subscribe = port.subscribe.bind(port);
    vi.spyOn(port, "subscribe").mockImplementation((onEvent, onGap, bootstrap) => { emit = onEvent; return subscribe(onEvent, onGap, bootstrap); });
    const balance = vi.spyOn(port, "balance");
    const { container } = mount(port);
    await waitFor(() => expect(figure(container)).toBe("¥110.00"));
    const reads = balance.mock.calls.length;
    act(() => emit({ kind: "notice", level: "info", text: "c", code: "display_currency", detail: "USD" } as WireEvent));
    await waitFor(() => expect(balance.mock.calls.length).toBeGreaterThan(reads));
  });

  it("shows no number when a refresh fails", async () => {
    const port = new MockPort();
    let fail = false;
    vi.spyOn(port, "balance").mockImplementation(async () => { if (fail) throw new Error("down"); return reading("¥110.00"); });
    let emit: (ev: WireEvent) => void = () => {};
    const subscribe = port.subscribe.bind(port);
    vi.spyOn(port, "subscribe").mockImplementation((onEvent, onGap, bootstrap) => { emit = onEvent; return subscribe(onEvent, onGap, bootstrap); });
    const { container } = mount(port);
    await waitFor(() => expect(figure(container)).toBe("¥110.00"));
    fail = true;
    act(() => emit({ kind: "notice", level: "info", text: "c", code: "display_currency", detail: "USD" } as WireEvent));
    await waitFor(() => expect(figure(container)).toBeNull());
  });
});

describe("a wallet the source reports as unavailable", () => {
  it("marks the figure without altering it", async () => {
    const port = new MockPort();
    vi.spyOn(port, "balance").mockResolvedValue(reading("¥-1.21", false));
    const { container } = mount(port);
    await waitFor(() => expect(figure(container)).toBe("¥-1.21"));
    const btn = container.querySelector(".studio-meter-wallet")!;
    expect(btn.getAttribute("data-short")).toBe("true");
    expect(btn.getAttribute("title")).toBe("余额不足");
    expect(btn.getAttribute("aria-label")).toContain("余额不足");
  });

  it("leaves an available wallet unmarked", async () => {
    const port = new MockPort();
    const { container } = mount(port);
    await waitFor(() => expect(figure(container)).toBe("¥110.00"));
    const btn = container.querySelector(".studio-meter-wallet")!;
    expect(btn.hasAttribute("data-short")).toBe(false);
    expect(btn.hasAttribute("title")).toBe(false);
  });
});
