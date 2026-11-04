// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { EditorView } from "@codemirror/view";
import "./testkit";
import { WorkbenchPanel } from "./WorkbenchPanel";
import { MockPort } from "../port/mock";

afterEach(cleanup);

async function dirtyReadme() {
  const user = userEvent.setup();
  const port = new MockPort();
  const saved = vi.spyOn(port, "saveWorkspaceFile");
  const { container } = render(<WorkbenchPanel port={port} tabs={[]} manual shown scheme="light" changes={[]} onCloseManual={vi.fn()} onSurfaces={vi.fn()} onExternal={vi.fn()} />);
  await user.click(await screen.findByRole("button", { name: "README.md" }));
  await user.click(screen.getByRole("button", { name: "编辑" }));
  const editor = await waitFor(() => {
    const el = container.querySelector(".cm-content");
    if (!el) throw new Error("editor not mounted");
    return EditorView.findFromDOM(el as HTMLElement)!;
  });
  act(() => editor.dispatch({ changes: { from: editor.state.doc.length, insert: "local draft" } }));
  await user.click(screen.getByRole("tab", { name: "README.md" }).querySelector<HTMLElement>(".workbench-tab-close")!);
  return { user, saved };
}

describe("closing a tab with unsaved text, as the WB-Frame board asks", () => {
  it("asks before closing, and keeps the tab and its text on cancel", async () => {
    const { user, saved } = await dirtyReadme();
    expect(screen.getByRole("alertdialog", { name: "README.md 还没保存" })).toBeTruthy();
    await user.click(within(screen.getByRole("alertdialog")).getByRole("button", { name: "取消" }));
    expect(screen.queryByRole("alertdialog")).toBeNull();
    expect(screen.getByRole("tab", { name: "README.md" })).toBeTruthy();
    expect(saved).not.toHaveBeenCalled();
  });

  it("saves and then closes", async () => {
    const { user, saved } = await dirtyReadme();
    await user.click(within(screen.getByRole("alertdialog")).getByRole("button", { name: "保存" }));
    await waitFor(() => expect(saved).toHaveBeenCalled());
    await waitFor(() => expect(screen.queryByRole("tab", { name: "README.md" })).toBeNull());
  });

  it("drops the text and closes without saving", async () => {
    const { user, saved } = await dirtyReadme();
    await user.click(within(screen.getByRole("alertdialog")).getByRole("button", { name: "不保存" }));
    await waitFor(() => expect(screen.queryByRole("tab", { name: "README.md" })).toBeNull());
    expect(saved).not.toHaveBeenCalled();
  });
});
