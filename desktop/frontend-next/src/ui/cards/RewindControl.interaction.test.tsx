// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { RewindControl } from "./RewindControl";

afterEach(cleanup);

it("offers only conversation when the whole rewind range has no files", async () => {
  render(<RewindControl cp={{ turn: 3, prompt: "empty range", files: 0 }} onPrepare={vi.fn()} onCommit={vi.fn()} />);
  await userEvent.click(screen.getByRole("button", { name: "回到这里" }));
  expect(screen.getAllByRole("menuitem")).toHaveLength(1);
  expect(screen.getByText("回退范围内未修改任何文件")).toBeTruthy();
});
