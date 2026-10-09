// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ExtensionSurface } from "../../port/wire";
import { Extensions } from "./Extensions";

afterEach(cleanup);

const panel = (title: string | undefined, progress: number | undefined): ExtensionSurface => ({
  pluginId: "sync-watch", surfaceId: "watch", sessionId: "sess-panel", generation: 7, kind: "panel",
  panel: {
    title, text: "Watching workspace", progress,
    fields: [{ key: "branch", value: "studio" }],
    actions: [{ actionId: "stop", label: "Stop sync" }],
  },
});

describe("standing extension panel progress", () => {
  it.each([0, 0.4, 1])("names titled progress at %s from the visible panel identity", (progress) => {
    const ui = render(<Extensions panels={[panel("Workspace sync", progress)]} onInvoke={vi.fn()} />);
    const bar = ui.getByRole("progressbar", { name: "Workspace sync" });
    expect(bar.getAttribute("aria-valuenow")).toBe(String(Math.round(progress * 100)));
    expect((bar.firstElementChild as HTMLElement).style.width).toBe(`${progress * 100}%`);
    expect(ui.getByText("Workspace sync")).toBeTruthy();
  });

  it.each([0, 0.4, 1])("names untitled progress at %s from the visible plugin identity", (progress) => {
    const ui = render(<Extensions panels={[panel(undefined, progress)]} onInvoke={vi.fn()} />);
    const bar = ui.getByRole("progressbar", { name: "sync-watch" });
    expect(bar.getAttribute("aria-valuenow")).toBe(String(Math.round(progress * 100)));
    expect(ui.getByText("sync-watch")).toBeTruthy();
  });

  it("updates the accessible identity and value when the same surface is republished", () => {
    const invoke = vi.fn();
    const ui = render(<Extensions panels={[panel("Workspace sync", 0.4)]} onInvoke={invoke} />);
    ui.rerender(<Extensions panels={[panel("Index rebuild", 0.7)]} onInvoke={invoke} />);
    expect(ui.queryByRole("progressbar", { name: "Workspace sync" })).toBeNull();
    expect(ui.getByRole("progressbar", { name: "Index rebuild" }).getAttribute("aria-valuenow")).toBe("70");
    ui.rerender(<Extensions panels={[panel(undefined, 1)]} onInvoke={invoke} />);
    expect(ui.getByRole("progressbar", { name: "sync-watch" }).getAttribute("aria-valuenow")).toBe("100");
  });

  it("keeps fields and the canonical action available by keyboard", async () => {
    const invoke = vi.fn();
    const ui = render(<Extensions panels={[panel("Workspace sync", 0.4)]} onInvoke={invoke} />);
    expect(ui.getByText("Watching workspace")).toBeTruthy();
    expect(ui.getByText("branch").tagName).toBe("DT");
    expect(ui.getByText("studio").tagName).toBe("DD");
    const user = userEvent.setup();
    await user.tab();
    expect(document.activeElement).toBe(ui.getByRole("button", { name: "Stop sync" }));
    await user.keyboard("{Enter}");
    expect(invoke.mock.calls).toEqual([["/sync-watch:stop"]]);
  });

  it("does not invent progress when the published panel has none", () => {
    const ui = render(<Extensions panels={[panel("Workspace sync", undefined)]} onInvoke={vi.fn()} />);
    expect(ui.queryByRole("progressbar")).toBeNull();
    expect(ui.getByRole("button", { name: "Stop sync" })).toBeTruthy();
  });

  it("keeps composed views and their action invocation beside standing panels", async () => {
    const invoke = vi.fn();
    const view: ExtensionSurface = {
      pluginId: "task-watch", surfaceId: "summary", kind: "view",
      view: { body: [{ kind: "text", value: "Task summary" }, { kind: "button", label: "Inspect task", actionId: "/task-watch:inspect" }] },
    };
    const ui = render(<Extensions panels={[panel("Workspace sync", 0.4)]} views={[view]} onInvoke={invoke} />);
    expect(ui.getByText("Task summary")).toBeTruthy();
    expect(ui.getAllByRole("progressbar")).toHaveLength(1);
    await userEvent.setup().click(ui.getByRole("button", { name: "Inspect task" }));
    expect(invoke.mock.calls).toEqual([["/task-watch:inspect"]]);
  });

  it("renders no extension block when there are no surfaces", () => {
    const ui = render(<Extensions panels={[]} onInvoke={vi.fn()} />);
    expect(ui.container.firstChild).toBeNull();
  });
});
