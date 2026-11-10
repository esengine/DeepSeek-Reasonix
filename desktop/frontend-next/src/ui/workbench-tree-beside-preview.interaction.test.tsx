// @vitest-environment jsdom
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import "./testkit";
import { MockPort } from "../port/mock";
import { WorkbenchPanel } from "./WorkbenchPanel";
import { boot, STORAGE } from "../i18n";

beforeEach(() => { localStorage.setItem(STORAGE, "zh"); boot(); });
afterEach(() => { cleanup(); vi.restoreAllMocks(); localStorage.clear(); });

const mount = (port = new MockPort()) =>
  render(<WorkbenchPanel port={port} tabs={[]} manual shown scheme="light" changes={[]} onCloseManual={vi.fn()} onSurfaces={vi.fn()} onExternal={vi.fn()} />);
const filesShown = (c: HTMLElement) => c.querySelector(".workbench-body")!.hasAttribute("data-files");

async function openTree(c: HTMLElement, user: ReturnType<typeof userEvent.setup>) {
  await user.click(c.querySelector('[data-action="workbench.files"]')!);
  await waitFor(() => expect(c.querySelector('[data-action-click="workbench.file"]')).toBeTruthy());
}

it("keeps the file tree in view when a file is picked from it", async () => {
  const user = userEvent.setup();
  const { container } = mount();
  await openTree(container, user);
  const row = container.querySelector<HTMLElement>('[data-action-click="workbench.file"]')!;
  const path = row.dataset.target!;
  await user.click(row);
  expect(screen.getAllByRole("tab").length).toBeGreaterThan(0);
  expect(filesShown(container)).toBe(true);
  expect(container.querySelector<HTMLElement>(`[data-action-click="workbench.file"][data-target="${path}"]`)).toBeTruthy();
});

it("lets the next file be picked from the same tree without reopening it", async () => {
  const user = userEvent.setup();
  const port = new MockPort();
  vi.spyOn(port, "workspaceFiles").mockResolvedValue({ files: ["a.md", "b.md"], directories: [] });
  const { container } = mount(port);
  await openTree(container, user);
  await user.click(container.querySelector<HTMLElement>('[data-target="a.md"]')!);
  await user.click(container.querySelector<HTMLElement>('[data-target="b.md"]')!);
  expect(filesShown(container)).toBe(true);
  expect(screen.getAllByRole("tab")).toHaveLength(2);
});

it("leaves the tree where the user put it when its file is closed", async () => {
  const user = userEvent.setup();
  const { container } = mount();
  await openTree(container, user);
  await user.click(container.querySelector<HTMLElement>('[data-action-click="workbench.file"]')!);
  await user.click(screen.getAllByRole("tab")[0].querySelector(".workbench-tab-close")!);
  expect(filesShown(container)).toBe(true);
});

it("does not bring the tree back after the user folded it", async () => {
  const user = userEvent.setup();
  const { container } = mount();
  await openTree(container, user);
  await user.click(container.querySelector<HTMLElement>('[data-action-click="workbench.file"]')!);
  await user.click(container.querySelector('[data-action="workbench.files"]')!);
  expect(filesShown(container)).toBe(false);
  await user.click(screen.getAllByRole("tab")[0].querySelector(".workbench-tab-close")!);
  expect(filesShown(container)).toBe(false);
});
