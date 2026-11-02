// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { OpenWithMenu } from "./OpenWithMenu";

const shell = vi.hoisted(() => ({ reveals: true }));
vi.mock("../port/host", () => ({ host: () => ({ revealsFiles: () => shell.reveals }) }));

afterEach(cleanup);

const targets = [
  { id: "vscode", name: "VS Code", available: true },
  { id: "terminal", name: "终端", available: true },
  { id: "sublime", name: "Sublime Text", available: false },
];

function draw(reveals = true) {
  shell.reveals = reveals;
  const openWith = vi.fn().mockResolvedValue(undefined);
  const openWithTargets = vi.fn().mockResolvedValue(targets);
  const onError = vi.fn();
  render(<OpenWithMenu hub={{ openWith, openWithTargets }} root="/w" onError={onError} />);
  return { openWith, openWithTargets, onError };
}

it("lists the programs installed on this machine and opens the project in the chosen one", async () => {
  const { openWith, openWithTargets } = draw();
  expect(openWithTargets).not.toHaveBeenCalled();
  await userEvent.click(screen.getByRole("button", { name: "用其他程序打开项目" }));
  await waitFor(() => expect(screen.getByRole("menuitem", { name: "用 VS Code 打开" })).toBeTruthy());
  expect(screen.getByRole("menuitem", { name: "用 终端 打开" })).toBeTruthy();
  expect(screen.queryByRole("menuitem", { name: /Sublime/ })).toBeNull();
  await userEvent.click(screen.getByRole("menuitem", { name: "用 终端 打开" }));
  expect(openWith).toHaveBeenCalledExactlyOnceWith("/w", "terminal");
  expect(screen.queryByRole("menu")).toBeNull();
});

it("says so through the error path when the program could not be started", async () => {
  const { openWith, onError } = draw();
  openWith.mockRejectedValueOnce(new Error("not installed"));
  await userEvent.click(screen.getByRole("button", { name: "用其他程序打开项目" }));
  await userEvent.click(await screen.findByRole("menuitem", { name: "用 VS Code 打开" }));
  await waitFor(() => expect(onError).toHaveBeenCalled());
});

it("closes on Escape and offers nothing where the kernel's machine cannot open programs", async () => {
  draw();
  await userEvent.click(screen.getByRole("button", { name: "用其他程序打开项目" }));
  await screen.findByRole("menu");
  await userEvent.keyboard("{Escape}");
  expect(screen.queryByRole("menu")).toBeNull();
  cleanup();
  draw(false);
  expect(screen.queryByRole("button", { name: "用其他程序打开项目" })).toBeNull();
});

it("says why an installed program is not offered, and also when nothing is", async () => {
  shell.reveals = true;
  const blocked = { id: "cursor", name: "Cursor", available: false, why: "inside_project", within: "/home/u" };
  const openWith = vi.fn().mockResolvedValue(undefined);
  const openWithTargets = vi.fn().mockResolvedValue([blocked]);
  render(<OpenWithMenu hub={{ openWith, openWithTargets }} root="/w" onError={vi.fn()} />);
  await userEvent.click(screen.getByRole("button", { name: "用其他程序打开项目" }));
  const item = await screen.findByRole("menuitem", { name: /用 Cursor 打开/ });
  expect(item.hasAttribute("disabled")).toBe(true);
  expect(item.getAttribute("title")).toContain("/home/u");
  expect(screen.queryByText("没有找到可用的程序")).toBeNull();
  await userEvent.click(item);
  expect(openWith).not.toHaveBeenCalled();
});
