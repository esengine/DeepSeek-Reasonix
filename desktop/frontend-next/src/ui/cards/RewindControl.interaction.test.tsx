// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { RewindControl } from "./RewindControl";
import type { RewindResult } from "../../port/port";

afterEach(cleanup);

it.each([
  { label: "mixed restore and delete", result: { written: ["a", "b"], deleted: ["c"] }, scope: "只还原代码", count: 3 },
  { label: "restore only", result: { written: ["a", "b", "c"] }, scope: "只还原代码", count: 3 },
  { label: "restore only with an empty deleted list", result: { written: ["a", "b", "c"], deleted: [] }, scope: "只还原代码", count: 3 },
  { label: "conversation only", result: {}, scope: "只回退对话", count: 0 },
])("counts committed files for $label", async ({ result, scope, count }) => {
  const onPrepare = vi.fn().mockResolvedValue({ planId: "plan", fileCount: 3, requiresConfirmation: false });
  const onCommit = vi.fn().mockResolvedValue({ ...result, transactionId: "tx", undoAvailable: true } satisfies RewindResult);
  render(<RewindControl cp={{ turn: 0, prompt: "no local edits", files: 3 }} onPrepare={onPrepare} onCommit={onCommit} onUndo={vi.fn()} />);
  await userEvent.click(screen.getByRole("button", { name: "回到这里" }));
  expect(screen.getAllByText("3 个文件")).toHaveLength(2);
  await userEvent.click(screen.getByRole("menuitem", { name: new RegExp(scope) }));
  expect(await screen.findByText(`已还原 ${count} 个文件`)).toBeTruthy();
});

it("offers only conversation when the whole rewind range has no files", async () => {
  render(<RewindControl cp={{ turn: 3, prompt: "empty range", files: 0 }} onPrepare={vi.fn()} onCommit={vi.fn()} onUndo={vi.fn()} />);
  await userEvent.click(screen.getByRole("button", { name: "回到这里" }));
  expect(screen.getAllByRole("menuitem")).toHaveLength(1);
  expect(screen.getByText("回退范围内未修改任何文件")).toBeTruthy();
});
