// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { WorkbenchSelectionMenu } from "./WorkbenchSelectionMenu";

afterEach(cleanup);

const menu = { editable: true, selected: true, canUndo: true, canRedo: false, quote: true, x: 10, y: 10 };

it("leads with adding the passage to the conversation, then editing, then history", () => {
  render(<WorkbenchSelectionMenu menu={menu} onAction={async () => {}} onClose={() => {}} />);
  expect(screen.getAllByRole("menuitem").map((b) => b.getAttribute("aria-label") ?? b.textContent?.replace(/[⌘⇧].*|Ctrl.*/, ""))).toEqual(
    ["加入对话", "剪切", "复制", "粘贴", "全选", "撤销", "重做"].map((w) => expect.stringContaining(w)),
  );
  expect(screen.getByRole("menuitem", { name: "复制" })).toBeTruthy();
  expect(screen.getByRole("menuitem", { name: /重做/ }).hasAttribute("disabled")).toBe(true);
});

it("offers only reading actions in a read-only view", () => {
  const onAction = vi.fn(async () => {});
  render(<WorkbenchSelectionMenu menu={{ ...menu, editable: false, quote: false }} onAction={onAction} onClose={() => {}} />);
  expect(screen.getAllByRole("menuitem")).toHaveLength(2);
  fireEvent.click(screen.getByRole("menuitem", { name: "复制" }));
  expect(onAction).toHaveBeenCalledWith("copy");
});
