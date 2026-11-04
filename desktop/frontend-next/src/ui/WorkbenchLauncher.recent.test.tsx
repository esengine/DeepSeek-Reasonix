// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { WorkbenchLauncher } from "./WorkbenchLauncher";

afterEach(cleanup);

const noop = () => {};

it("lists recently opened files and opens the one picked", async () => {
  const open = vi.fn();
  render(<WorkbenchLauncher changes={0} onFiles={noop} onFind={noop} onBrowser={noop} recent={["src/a.ts", "docs/b.md"]} onOpenFile={open} />);
  const rows = document.querySelectorAll('[data-action="workbench.launch-recent"]');
  expect([...rows].map((r) => r.getAttribute("data-target"))).toEqual(["src/a.ts", "docs/b.md"]);
  await userEvent.click(screen.getByText("a.ts"));
  expect(open).toHaveBeenCalledWith("src/a.ts");
});

it("shows no recent list when nothing was opened", () => {
  render(<WorkbenchLauncher changes={0} onFiles={noop} onFind={noop} onBrowser={noop} recent={[]} onOpenFile={noop} />);
  expect(document.querySelector(".workbench-launcher-recent")).toBeNull();
});

it("offers the explorer with the session's change count, finding a file, and the browser, as the board draws a new tab", async () => {
  const find = vi.fn();
  render(<WorkbenchLauncher changes={4} onFiles={noop} onFind={find} onBrowser={noop} />);
  expect([...document.querySelectorAll(".workbench-launcher > button b")].map((b) => b.textContent)).toEqual(["资源管理器4", "文件…", "浏览器"]);
  await userEvent.click(screen.getByRole("button", { name: "文件…" }));
  expect(find).toHaveBeenCalled();
});
