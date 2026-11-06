// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import "../testkit";
import { RememberCard } from "./RememberCard";

afterEach(cleanup);

const fact = { name: "project/gbk.md", title: "导出文件一律 GBK 解码", description: "", scope: "project", activation: "pinned" } as never;

it("says it is remembered, with where and how, and offers to forget", () => {
  render(<RememberCard m={fact} onForget={vi.fn()} />);
  expect(screen.getByText("记住了")).toBeTruthy();
  expect(screen.getByText(/只在这个项目 · 每次都带上/)).toBeTruthy();
  expect(screen.getByRole("button", { name: "忘记" })).toBeTruthy();
});

it("after forgetting, says so, drops how it was used and has no button left", () => {
  render(<RememberCard m={fact} forgotten onForget={vi.fn()} />);
  expect(screen.getByText("已经忘掉")).toBeTruthy();
  expect(document.querySelector(".note-meta")?.textContent).toBe("· 只在这个项目");
  expect(screen.queryByRole("button", { name: "忘记" })).toBeNull();
});
