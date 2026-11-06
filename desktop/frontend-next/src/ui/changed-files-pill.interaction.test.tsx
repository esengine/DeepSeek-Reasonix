// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import "./testkit";
import { Pane } from "./Pane";
import { ChangedFilesSetting } from "./ChangedFilesSetting";
import { MockPort } from "../port/mock";
import { boot, STORAGE } from "../i18n";
import type { AgentPort } from "../port/port";
import type { RuntimeView } from "../port/hub";
import type { WireEvent } from "../port/wire";
import type { WorkspaceChange } from "../port/workspace";

afterEach(() => {
  cleanup();
  localStorage.setItem(STORAGE, "zh");
  boot();
  vi.restoreAllMocks();
});

const rt = { id: "p1", root: "/w", name: "w" } as RuntimeView;
const change = (path: string, insertions?: number, deletions?: number): WorkspaceChange => ({ path, status: "M", insertions, deletions });
const props = {
  rt, title: "w", active: true, visible: true, sideHost: null, side: false,
  onFocus() {}, onReport() {}, onSessionChanged() {}, pulse: 0, findPulse: 0,
  onSettings() {}, needsProject: false, onOpenProject() {}, onKeepHere() {},
  theme: "dark", dockW: 560, dockMax: 880, onDockW() {},
};

async function open(changes: WorkspaceChange[] | null, over: { port?: MockPort; repo?: boolean; show?: boolean; running?: boolean; onManualBrowser?: (on: boolean) => void } = {}) {
  if (over.port) vi.restoreAllMocks();
  const port = over.port ?? new MockPort();
  vi.spyOn(port, "changes").mockImplementation(async () => ({ repo: over.repo ?? true, changes: changes ?? [] }));
  if (over.show === false) await port.saveChangedFilesSettings(false);
  if (over.running) {
    const status = await port.status();
    vi.spyOn(port, "status").mockResolvedValue({ ...status, running: true });
  }
  let emit: (ev: WireEvent) => void = () => {};
  const subscribe = port.subscribe.bind(port);
  vi.spyOn(port, "subscribe").mockImplementation((onEvent, onGap, bootstrap) => {
    emit = onEvent;
    return subscribe(onEvent, onGap, bootstrap);
  });
  const view = render(<Pane {...props} port={port as AgentPort} onManualBrowser={over.onManualBrowser} />);
  await waitFor(() => expect(port.changes).toHaveBeenCalled());
  return { port, view, send: (ev: object) => act(() => emit(ev as WireEvent)) };
}

const pill = () => document.querySelector<HTMLButtonElement>('[data-action="changes.open"]');
const box = () => document.querySelector<HTMLTextAreaElement>('textarea[aria-label="任务输入"]')!;

describe("the changed-files pill above the input box", () => {
  it("sums what git counted into one pill and moves the count out of the branch chip", async () => {
    await open([change("a.go", 10, 2), change("b.go", 5, 1), change("c.png")]);
    await waitFor(() => expect(pill()).toBeTruthy());
    expect(pill()!.textContent).toBe("3 个文件已更改+15−3");
    expect(pill()!.getAttribute("aria-label")).toContain("新增 15 行，删除 3 行");
    expect(document.querySelector(".studio-branch small")).toBeNull();
  });

  it("is drawn right above the input box, outside the queue and notice strips", async () => {
    await open([change("a.go", 1, 0)]);
    await waitFor(() => expect(pill()).toBeTruthy());
    const row = pill()!.parentElement!;
    expect(row.closest(".composeaux, .composenotes")).toBeNull();
    expect(row.closest(".compose")).toBeTruthy();
    expect(row.compareDocumentPosition(box()) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    expect(document.querySelector(".composeaux")!.compareDocumentPosition(row) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  });

  it("draws nothing for a clean tree, a workspace without git, or a tree that has not answered", async () => {
    await open([]);
    await act(async () => {});
    expect(pill()).toBeNull();
    cleanup();
    await open([change("a.go", 1, 1)], { repo: false });
    await act(async () => {});
    expect(pill()).toBeNull();
    expect(document.querySelector(".studio-branch small")).toBeNull();
  });

  it("keeps a binary-only change free of invented line counts", async () => {
    await open([change("logo.png")]);
    await waitFor(() => expect(pill()).toBeTruthy());
    expect(pill()!.textContent).toBe("1 个文件已更改");
  });

  it("abbreviates large totals", async () => {
    await open([change("a.go", 1200, 15400), change("b.go", 40, 0)]);
    await waitFor(() => expect(pill()).toBeTruthy());
    expect(pill()!.textContent).toBe("2 个文件已更改+1.2k−15k");
  });

  it("uses the English singular and plural", async () => {
    localStorage.setItem(STORAGE, "en");
    boot();
    await open([change("a.go", 2, 1)]);
    await waitFor(() => expect(pill()).toBeTruthy());
    expect(pill()!.textContent).toBe("1 file changed+2−1");
    cleanup();
    await open([change("a.go", 2, 1), change("b.go", 1, 1)]);
    await waitFor(() => expect(pill()).toBeTruthy());
    expect(pill()!.textContent).toBe("2 files changed+3−2");
  });

  it("follows the tree after a turn ends", async () => {
    const list = [change("a.go", 1, 0)];
    const pane = await open(list);
    await waitFor(() => expect(pill()!.textContent).toContain("1 个文件已更改"));
    list.push(change("b.go", 4, 4));
    pane.send({ kind: "turn_started" });
    pane.send({ kind: "turn_done" });
    await waitFor(() => expect(pill()!.textContent).toBe("2 个文件已更改+5−4"));
  });

  it("opens the workbench's changes page, and again after that page was closed", async () => {
    const onManualBrowser = vi.fn();
    await open([change("a.go", 1, 0)], { onManualBrowser });
    await waitFor(() => expect(pill()).toBeTruthy());
    const changesTab = () => document.querySelector('[role="tab"][aria-selected="true"]')?.textContent?.startsWith("改动") ?? false;
    fireEvent.click(pill()!);
    expect(onManualBrowser).toHaveBeenCalledWith(true);
    await waitFor(() => expect(changesTab()).toBe(true));
    expect(document.querySelector(".wb-changes")).toBeTruthy();
    fireEvent.click(document.querySelector('[data-action="workbench.close"][data-target="changes:"]')!);
    await waitFor(() => expect(document.querySelector(".wb-changes")).toBeNull());
    fireEvent.click(pill()!);
    await waitFor(() => expect(changesTab()).toBe(true));
  });

  it("does not open the file list on mount or when the tree refreshes", async () => {
    const pane = await open([change("a.go", 1, 0)]);
    await waitFor(() => expect(pill()).toBeTruthy());
    pane.send({ kind: "turn_started" });
    pane.send({ kind: "turn_done" });
    await act(async () => {});
    expect(document.querySelector(".workbench-body[data-files]")).toBeNull();
  });

  it("sits with a held queue line and a runtime notice without either leaving", async () => {
    const pane = await open([change("a.go", 1, 0)], { running: true });
    await waitFor(() => expect(pill()).toBeTruthy());
    vi.spyOn(pane.port, "steer").mockImplementation((text) => pane.port.queueFollowup(text));
    fireEvent.change(box(), { target: { value: "next thing", selectionStart: 10 } });
    fireEvent.click(await screen.findByRole("button", { name: "插话" }));
    await screen.findByRole("button", { name: "撤回" });
    pane.send({ kind: "notice", level: "warn", audience: "operator", code: "x.y", text: "运行时提示" });
    await screen.findByText("运行时提示");
    expect(pill()).toBeTruthy();
    expect(screen.getByRole("button", { name: "撤回" }).closest(".queue")).toBeTruthy();
    expect(pill()!.closest(".queue, .composeaux, .composenotes")).toBeNull();
    expect(document.querySelector(".composenotes")!.contains(screen.getByText("运行时提示"))).toBe(true);
  });

  it("is per kernel: a pane whose kernel turned it off does not hide another's", async () => {
    const off = new MockPort();
    await off.saveChangedFilesSettings(false);
    await open([change("a.go", 1, 0)], { port: off });
    await act(async () => {});
    expect(pill()).toBeNull();
    cleanup();
    await open([change("a.go", 1, 0)]);
    await waitFor(() => expect(pill()).toBeTruthy());
  });
});

describe("the setting behind the pill", () => {
  it("is on by default and, turned off, brings the count back beside the branch", async () => {
    const port = new MockPort();
    const pane = await open([change("a.go", 1, 0), change("b.go", 1, 0)], { port });
    await waitFor(() => expect(pill()).toBeTruthy());
    pane.view.unmount();
    const settings = render(<ChangedFilesSetting port={port as AgentPort} />);
    const sw = settings.container.querySelector<HTMLButtonElement>('[data-action="chrome.changed-files"]')!;
    expect(sw.getAttribute("aria-checked")).toBe("true");
    fireEvent.click(sw);
    await waitFor(() => expect(sw.getAttribute("aria-checked")).toBe("false"));
    expect((await port.changedFilesSettings()).show).toBe(false);
    settings.unmount();
    await open([change("a.go", 1, 0), change("b.go", 1, 0)], { port });
    await act(async () => {});
    expect(pill()).toBeNull();
    expect(await screen.findByText("2 个变更")).toBeTruthy();
  });

  it("reads the kernel's answer when a pane mounts", async () => {
    const port = new MockPort();
    await port.saveChangedFilesSettings(false);
    await open([change("a.go", 1, 0)], { port });
    await act(async () => {});
    expect(pill()).toBeNull();
  });

  it("hides at once when switched off in a mounted pane, and returns when switched on", async () => {
    const port = new MockPort();
    await open([change("a.go", 1, 0)], { port });
    await waitFor(() => expect(pill()).toBeTruthy());
    const settings = render(<ChangedFilesSetting port={port as AgentPort} />);
    const sw = settings.container.querySelector<HTMLButtonElement>('[data-action="chrome.changed-files"]')!;
    fireEvent.click(sw);
    await waitFor(() => expect(pill()).toBeNull());
    fireEvent.click(sw);
    await waitFor(() => expect(pill()).toBeTruthy());
  });

  it("puts the switch back and says why when the kernel refuses the save", async () => {
    const port = new MockPort();
    vi.spyOn(port, "saveChangedFilesSettings").mockRejectedValue(new Error("disk full"));
    const settings = render(<ChangedFilesSetting port={port as AgentPort} />);
    const sw = settings.container.querySelector<HTMLButtonElement>('[data-action="chrome.changed-files"]')!;
    fireEvent.click(sw);
    expect(await screen.findByText(/disk full/)).toBeTruthy();
    expect(sw.getAttribute("aria-checked")).toBe("true");
  });
});
