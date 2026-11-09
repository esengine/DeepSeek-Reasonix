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
  render(<WorkbenchLauncher changes={0} onFiles={noop} onBrowser={noop} recent={["src/a.ts", "docs/b.md"]} onOpenFile={open} />);
  const rows = document.querySelectorAll('[data-action="workbench.launch-recent"]');
  expect([...rows].map((r) => r.getAttribute("data-target"))).toEqual(["src/a.ts", "docs/b.md"]);
  await userEvent.click(screen.getByText("a.ts"));
  expect(open).toHaveBeenCalledWith("src/a.ts");
});

it("shows no recent list when nothing was opened", () => {
  render(<WorkbenchLauncher changes={0} onFiles={noop} onBrowser={noop} recent={[]} onOpenFile={noop} />);
  expect(document.querySelector(".workbench-launcher-recent")).toBeNull();
});

it("offers one explorer entry with the change count alongside the browser", async () => {
  const files = vi.fn();
  render(<WorkbenchLauncher changes={4} onFiles={files} onBrowser={noop} />);
  expect([...document.querySelectorAll(".workbench-launcher > button b")].map((b) => b.textContent)).toEqual(["资源管理器4", "浏览器"]);
  await userEvent.click(screen.getByRole("button", { name: "资源管理器" }));
  expect(files).toHaveBeenCalledOnce();
});
