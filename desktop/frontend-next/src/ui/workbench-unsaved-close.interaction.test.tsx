// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { EditorView } from "@codemirror/view";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { MockPort } from "../port/mock";
import { WorkbenchPanel } from "./WorkbenchPanel";

afterEach(cleanup);

type User = ReturnType<typeof userEvent.setup>;

function mount(port: MockPort) {
  return render(<WorkbenchPanel port={port} tabs={[]} manual={false} shown scheme="light" changes={[]} onCloseManual={vi.fn()} onSurfaces={vi.fn()} onExternal={vi.fn()} />);
}

async function editOpenREADME(user: User) {
  await user.click(screen.getByRole("button", { name: "编辑" }));
  const content = await waitFor(() => {
    const el = document.querySelector(".cm-content");
    if (!el) throw new Error("editor not mounted");
    return el as HTMLElement;
  });
  const view = EditorView.findFromDOM(content)!;
  act(() => view.dispatch({ changes: { from: view.state.doc.length, insert: "\n\n## Unsaved" } }));
  return view;
}

async function openEditedREADME(user: User) {
  await user.click(await screen.findByRole("button", { name: "README.md" }));
  await waitFor(() => expect(document.querySelector(".workbench-read .md")).toBeTruthy());
  return editOpenREADME(user);
}

describe("saving a file without the bar's button", () => {
  it("offers no save button and writes the draft on Ctrl+S", async () => {
    const user = userEvent.setup();
    const port = new MockPort();
    const save = vi.spyOn(port, "saveWorkspaceFile");
    mount(port);
    await openEditedREADME(user);

    expect(screen.queryByRole("button", { name: "保存" })).toBeNull();

    fireEvent.keyDown(window, { key: "s", ctrlKey: true });
    await waitFor(() => expect(save).toHaveBeenCalledTimes(1));
    expect(save.mock.calls[0][0].content.endsWith("## Unsaved")).toBe(true);
  });

  it("leaves Ctrl+S alone for a panel that is not showing", async () => {
    const user = userEvent.setup();
    const port = new MockPort();
    const save = vi.spyOn(port, "saveWorkspaceFile");
    const props = { port, tabs: [], manual: false, scheme: "light" as const, changes: [], onCloseManual: vi.fn(), onSurfaces: vi.fn(), onExternal: vi.fn() };
    const view = render(<WorkbenchPanel {...props} shown />);
    await openEditedREADME(user);

    view.rerender(<WorkbenchPanel {...props} shown={false} />);
    fireEvent.keyDown(window, { key: "s", ctrlKey: true });
    await new Promise((resolve) => setTimeout(resolve, 20));
    expect(save).not.toHaveBeenCalled();
  });
});

describe("closing a tab with unsaved edits", () => {
  it("turns the close mark into a dot only while the draft differs from the disk", async () => {
    const user = userEvent.setup();
    const port = new MockPort();
    mount(port);
    await user.click(await screen.findByRole("button", { name: "README.md" }));
    await screen.findByRole("button", { name: "编辑" });
    expect(screen.getByRole("button", { name: "关闭 README.md" }).hasAttribute("data-dirty")).toBe(false);

    await editOpenREADME(user);
    const close = screen.getByRole("button", { name: "关闭 README.md" });
    expect(close.hasAttribute("data-dirty")).toBe(true);
    expect(close.querySelector(".workbench-tab-dot")).toBeTruthy();
  });

  it("asks before closing, and cancel keeps the tab and the draft", async () => {
    const user = userEvent.setup();
    const port = new MockPort();
    mount(port);
    await openEditedREADME(user);

    await user.click(screen.getByRole("button", { name: "关闭 README.md" }));
    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByText("确认未保存就关闭它吗？")).toBeTruthy();
    expect(within(dialog).getByText("这可能引起你的项目出错。")).toBeTruthy();
    expect(within(dialog).getByRole("button", { name: "不保存" })).toBeTruthy();
    expect(within(dialog).getByRole("button", { name: "保存" })).toBeTruthy();
    expect(within(dialog).getByRole("button", { name: "取消" })).toBeTruthy();

    await user.click(within(dialog).getByRole("button", { name: "取消" }));
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(screen.getByRole("tab", { name: "README.md" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "关闭 README.md" }).hasAttribute("data-dirty")).toBe(true);
  });

  it("closes without writing when the answer is 不保存, and the draft is dropped", async () => {
    const user = userEvent.setup();
    const port = new MockPort();
    const save = vi.spyOn(port, "saveWorkspaceFile");
    mount(port);
    await openEditedREADME(user);

    await user.click(screen.getByRole("button", { name: "关闭 README.md" }));
    await user.click(await screen.findByRole("button", { name: "不保存" }));
    await waitFor(() => expect(screen.queryByRole("tab", { name: "README.md" })).toBeNull());
    expect(save).not.toHaveBeenCalled();

    // Reopening reads the disk: the refused draft does not come back.
    await user.click(await screen.findByRole("button", { name: "README.md" }));
    await waitFor(() => expect(document.querySelector(".workbench-read .md")?.textContent ?? "").not.toContain("Unsaved"));
  });

  it("writes the draft and then closes when the answer is 保存", async () => {
    const user = userEvent.setup();
    const port = new MockPort();
    const save = vi.spyOn(port, "saveWorkspaceFile");
    mount(port);
    await openEditedREADME(user);

    await user.click(screen.getByRole("button", { name: "关闭 README.md" }));
    await user.click(await screen.findByRole("button", { name: "保存" }));
    await waitFor(() => expect(save).toHaveBeenCalledTimes(1));
    expect(save.mock.calls[0][0].content.endsWith("## Unsaved")).toBe(true);
    await waitFor(() => expect(screen.queryByRole("tab", { name: "README.md" })).toBeNull());
  });
});
