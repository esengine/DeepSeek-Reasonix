// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import "./testkit";
import { useCallback, useRef, useState, type ReactElement } from "react";
import { Pane } from "./Pane";
import { ViewSwitch } from "./ViewSwitch";
import type { PaneReport } from "./panetypes";
import { usePaneViewOf, type PaneView } from "../state/paneview";
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

// The window's view switch beside one pane, wired as App wires it: the pane
// reports its view, and a press goes back to it as a counted request.
function Harness({ paneKey, onReport, ...rest }: Record<string, any>) {
  const [rep, setRep] = useState({ view: "flow" as PaneView, rows: 0, surfaces: 0 });
  const [req, setReq] = useState<{ to: PaneView; n: number } | undefined>();
  const outer = useRef(onReport);
  outer.current = onReport;
  const report = useCallback((id: string, r: PaneReport) => {
    setRep((was) => (was.view === r.view && was.rows === r.rows && was.surfaces === r.surfaces ? was : { view: r.view, rows: r.rows, surfaces: r.surfaces }));
    outer.current?.(id, r);
  }, []);
  return (
    <>
      <ViewSwitch view={rep.view} rows={rep.rows} surfaces={rep.surfaces} onView={(to) => setReq((r) => ({ to, n: (r?.n ?? 0) + 1 }))} />
      <Pane key={paneKey} {...(rest as any)} viewRequest={req} onReport={report} />
    </>
  );
}
const boxOf = (id: string, el: ReactElement) => <div data-harness={id} key={id}>{el}</div>;

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
  const view = render(boxOf(id, <Harness {...all} />));
  return { port, view, all, send: (ev: Partial<WireEvent>) => act(() => emit(ev as WireEvent)) };
}

const button = (to: string, root: ParentNode = document) => root.querySelector<HTMLButtonElement>(`[data-action="pane.view"][data-value="${to}"]`)!;
const selectedIn = (root: ParentNode) => root.querySelector('[data-action="pane.view"][aria-pressed="true"]')?.getAttribute("data-value") ?? "flow";
const selected = () => selectedIn(document);
const toConversation = (root: ParentNode = document) => fireEvent.click(root.querySelector('[data-action="pane.view"][aria-pressed="true"]')!);
const flowHidden = () => document.querySelector(".compose")!.hasAttribute("hidden");
const box = () => document.querySelector<HTMLTextAreaElement>('textarea[aria-label="任务输入"]')!;

describe("a pane's view", () => {
  it("refreshes the open conversation's context after model settings change", async () => {
    const port = new MockPort();
    const context = port.context.bind(port);
    let window = 160_000;
    vi.spyOn(port, "context").mockImplementation(async () => ({ ...await context(), window }));
    const all = { ...props, rt: rtOf("p1"), port: port as AgentPort };
    const view = render(<Pane {...all} />);
    await waitFor(() => expect(document.querySelector(".chrome-context-total")?.textContent).toContain("160K"));

    window = 1_000_000;
    view.rerender(boxOf("p1", <Harness {...all} pulse={1} />));
    await waitFor(() => expect(document.querySelector(".chrome-context-total")?.textContent).toContain("1.0M"));
  });

  it("opens on the conversation with the composer showing", () => {
    mount();
    expect(selected()).toBe("flow");
    expect(flowHidden()).toBe(false);
  });

  it("offers the run analysis only after a turn has rows, and switches to and from it", async () => {
    const { send } = mount();
    expect(button("analysis")).toBeNull();
    send({ kind: "turn_started" });
    await waitFor(() => expect(button("analysis")).not.toBeNull());
    fireEvent.click(button("analysis"));
    expect(selected()).toBe("analysis");
    expect(flowHidden()).toBe(true);
    expect(document.querySelector('.scroll[data-pane="analysis"]')!.hasAttribute("hidden")).toBe(false);
    toConversation();
    expect(selected()).toBe("flow");
    expect(flowHidden()).toBe(false);
  });

  it("keeps a half-written prompt through a visit to the analysis", async () => {
    const { send } = mount();
    fireEvent.change(box(), { target: { value: "draft text", selectionStart: 10 } });
    send({ kind: "turn_started" });
    await waitFor(() => expect(button("analysis")).not.toBeNull());
    fireEvent.click(button("analysis"));
    toConversation();
    expect(box().value).toBe("draft text");
  });

  it("keeps the view across a re-render with new props and while hidden", async () => {
    const { send, view, all } = mount();
    send({ kind: "turn_started" });
    await waitFor(() => expect(button("analysis")).not.toBeNull());
    fireEvent.click(button("analysis"));
    view.rerender(boxOf("p1", <Harness {...all} pulse={1} />));
    expect(selected()).toBe("analysis");
    view.rerender(boxOf("p1", <Harness {...all} pulse={1} active={false} visible={false} />));
    view.rerender(boxOf("p1", <Harness {...all} pulse={1} />));
    expect(selected()).toBe("analysis");
  });

  it("asks the window to open the workbench dock and keeps the conversation showing", () => {
    const onManualBrowser = vi.fn();
    mount("p1", { onManualBrowser });
    fireEvent.click(button("browser"));
    expect(onManualBrowser).toHaveBeenLastCalledWith(true);
    expect(flowHidden()).toBe(false);
  });

  it("shows the workbench as the selected view while the dock is open, and closes it from the conversation tab", () => {
    const onManualBrowser = vi.fn();
    mount("p1", { onManualBrowser, manualBrowser: true });
    expect(selected()).toBe("browser");
    toConversation();
    expect(onManualBrowser).toHaveBeenLastCalledWith(false);
  });

  it("gives each pane its own view", async () => {
    const a = mount("a");
    const b = render(boxOf("b", <Harness {...props} rt={rtOf("b")} port={new MockPort() as AgentPort} />));
    a.send({ kind: "turn_started" });
    const [first, second] = Array.from(document.querySelectorAll("[data-harness]")) as HTMLElement[];
    await waitFor(() => expect(button("analysis", first)).not.toBeNull());
    fireEvent.click(button("analysis", first));
    expect(selectedIn(first)).toBe("analysis");
    expect(selectedIn(second)).toBe("flow");
    b.unmount();
  });

  it("starts again on the conversation when the pane is remounted for a takeover", async () => {
    const first = mount("p1");
    first.send({ kind: "turn_started" });
    await waitFor(() => expect(button("analysis")).not.toBeNull());
    fireEvent.click(button("analysis"));
    expect(selected()).toBe("analysis");
    cleanup();
    const again = mount("p1");
    await waitFor(() => expect(again.port).toBeTruthy());
    expect(selected()).toBe("flow");
  });

  it("starts again on the conversation when a new key replaces a mounted pane", async () => {
    const { send, view, all } = mount("p1");
    send({ kind: "turn_started" });
    await waitFor(() => expect(button("analysis")).not.toBeNull());
    fireEvent.click(button("analysis"));
    view.rerender(boxOf("p1", <Harness paneKey="p1:1" {...all} />));
    expect(selected()).toBe("flow");
    fireEvent.click(button("browser"));
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
    await waitFor(() => expect(button("analysis")).not.toBeNull());
    fireEvent.click(button("analysis"));
    expect(screen.getByTestId("probe").textContent).toBe("analysis");
    view.unmount();
    expect(screen.getByTestId("probe").textContent).toBe("flow");
    probe.unmount();
    expect(all.rt.id).toBe("p1");
  });

  it("leaves analysis for an external files request and does not replay it after a takeover", async () => {
    const onManualBrowser = vi.fn();
    const { send, view, all } = mount("p1", { onManualBrowser });
    send({ kind: "turn_started" });
    await waitFor(() => expect(button("analysis")).not.toBeNull());
    fireEvent.click(button("analysis"));
    view.rerender(boxOf("p1", <Harness {...all} filesPulse={1} />));
    expect(selected()).toBe("flow");
    expect(onManualBrowser).toHaveBeenLastCalledWith(true);
    expect(document.querySelector(".workbench-body")?.hasAttribute("data-files")).toBe(true);
    onManualBrowser.mockClear();
    view.rerender(boxOf("p1", <Harness paneKey="p1:1" {...all} filesPulse={1} />));
    expect(onManualBrowser).not.toHaveBeenCalled();
    expect(document.querySelector(".workbench-body")?.hasAttribute("data-files")).toBe(false);
  });

});
