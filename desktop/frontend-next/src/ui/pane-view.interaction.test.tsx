// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import "./testkit";
import { Pane } from "./Pane";
import { usePaneViewOf } from "../state/paneview";
import { MockPort } from "../port/mock";
import type { AgentPort } from "../port/port";
import type { RuntimeView } from "../port/hub";
import type { WireEvent } from "../port/wire";

afterEach(() => { cleanup(); localStorage.clear(); vi.restoreAllMocks(); });

const props = {
  title: "w", active: true, visible: true, sideHost: null, side: false,
  onFocus() {}, onReport() {}, onSessionChanged() {}, pulse: 0, findPulse: 0,
  onSettings() {}, needsProject: false, onOpenProject() {}, onKeepHere() {},
  theme: "dark", dockW: 560, dockMax: 880, onDockW() {},
};
const rtOf = (id: string) => ({ id, root: "/w", name: "w" }) as RuntimeView;

// A pane whose stream the test holds, so a turn can start and the run
// analysis tab (offered only once there are trajectory rows) can appear.
function mount(id = "p1", extra: Record<string, unknown> = {}) {
  const port = new MockPort();
  let emit: (ev: WireEvent) => void = () => {};
  const subscribe = port.subscribe.bind(port);
  vi.spyOn(port, "subscribe").mockImplementation((onEvent, onGap, bootstrap) => {
    emit = onEvent;
    return subscribe(onEvent, onGap, bootstrap);
  });
  const all = { ...props, rt: rtOf(id), port: port as AgentPort, ...extra };
  const view = render(<Pane {...all} />);
  return { port, view, all, send: (ev: Partial<WireEvent>) => act(() => emit(ev as WireEvent)) };
}

const tab = (name: string) => screen.getByRole("tab", { name: new RegExp(name) });
const selected = () => screen.getAllByRole("tab").find((el) => el.getAttribute("aria-selected") === "true")?.getAttribute("data-value");
const flowHidden = () => document.querySelector(".compose")!.hasAttribute("hidden");
const box = () => document.querySelector<HTMLTextAreaElement>('textarea[aria-label="任务输入"]')!;

describe("a pane's view", () => {
  it("opens on the conversation with the composer showing", () => {
    mount();
    expect(selected()).toBe("flow");
    expect(flowHidden()).toBe(false);
  });

  it("offers the run analysis only after a turn has rows, and switches to and from it", async () => {
    const { send } = mount();
    expect(screen.queryByRole("tab", { name: /运行分析/ })).toBeNull();
    send({ kind: "turn_started" });
    fireEvent.click(await screen.findByRole("tab", { name: /运行分析/ }));
    expect(selected()).toBe("analysis");
    expect(flowHidden()).toBe(true);
    expect(document.querySelector('.scroll[data-pane="analysis"]')!.hasAttribute("hidden")).toBe(false);
    fireEvent.click(tab("对话"));
    expect(selected()).toBe("flow");
    expect(flowHidden()).toBe(false);
  });

  it("keeps a half-written prompt through a visit to the analysis", async () => {
    const { send } = mount();
    fireEvent.change(box(), { target: { value: "draft text", selectionStart: 10 } });
    send({ kind: "turn_started" });
    fireEvent.click(await screen.findByRole("tab", { name: /运行分析/ }));
    fireEvent.click(tab("对话"));
    expect(box().value).toBe("draft text");
  });

  it("keeps the view across a re-render with new props and while hidden", async () => {
    const { send, view, all } = mount();
    send({ kind: "turn_started" });
    fireEvent.click(await screen.findByRole("tab", { name: /运行分析/ }));
    view.rerender(<Pane {...all} pulse={1} />);
    expect(selected()).toBe("analysis");
    view.rerender(<Pane {...all} pulse={1} active={false} visible={false} />);
    view.rerender(<Pane {...all} pulse={1} />);
    expect(selected()).toBe("analysis");
  });

  it("asks the window to open the workbench dock and keeps the conversation showing", () => {
    const onManualBrowser = vi.fn();
    mount("p1", { onManualBrowser });
    fireEvent.click(tab("工作台"));
    expect(onManualBrowser).toHaveBeenLastCalledWith(true);
    expect(flowHidden()).toBe(false);
  });

  it("shows the workbench as the selected view while the dock is open, and closes it from the conversation tab", () => {
    const onManualBrowser = vi.fn();
    mount("p1", { onManualBrowser, manualBrowser: true });
    expect(selected()).toBe("browser");
    fireEvent.click(tab("对话"));
    expect(onManualBrowser).toHaveBeenLastCalledWith(false);
  });

  it("gives each pane its own view", async () => {
    const a = mount("a");
    mount("b");
    a.send({ kind: "turn_started" });
    const [first, second] = Array.from(document.querySelectorAll("section.pane")) as HTMLElement[];
    fireEvent.click(await within(first).findByRole("tab", { name: /运行分析/ }));
    const chosen = (el: HTMLElement) => within(el).getAllByRole("tab").find((t) => t.getAttribute("aria-selected") === "true")?.getAttribute("data-value");
    expect(chosen(first)).toBe("analysis");
    expect(chosen(second)).toBe("flow");
  });

  it("starts again on the conversation when the pane is remounted for a takeover", async () => {
    const first = mount("p1");
    first.send({ kind: "turn_started" });
    fireEvent.click(await screen.findByRole("tab", { name: /运行分析/ }));
    expect(selected()).toBe("analysis");
    cleanup();
    const again = mount("p1");
    await waitFor(() => expect(again.port).toBeTruthy());
    expect(selected()).toBe("flow");
  });

  it("starts again on the conversation when a new key replaces a mounted pane", async () => {
    const { send, view, all } = mount("p1");
    send({ kind: "turn_started" });
    fireEvent.click(await screen.findByRole("tab", { name: /运行分析/ }));
    view.rerender(<Pane key="p1:1" {...all} />);
    expect(selected()).toBe("flow");
    fireEvent.click(tab("工作台"));
    expect(selected()).toBe("flow");
  });

  it("reads back from outside the pane as the one source", async () => {
    function Probe({ id }: { id: string }) {
      return <output data-testid="probe">{usePaneViewOf(id)}</output>;
    }
    const { send, view, all } = mount("p1");
    const probe = render(<Probe id="p1" />);
    expect(screen.getByTestId("probe").textContent).toBe("flow");
    send({ kind: "turn_started" });
    fireEvent.click(await screen.findByRole("tab", { name: /运行分析/ }));
    expect(screen.getByTestId("probe").textContent).toBe("analysis");
    view.unmount();
    expect(screen.getByTestId("probe").textContent).toBe("flow");
    probe.unmount();
    expect(all.rt.id).toBe("p1");
  });
});
