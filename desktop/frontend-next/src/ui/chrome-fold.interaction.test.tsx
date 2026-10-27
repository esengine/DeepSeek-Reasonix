// @vitest-environment jsdom
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { act, cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { App } from "./App";
import { MockHub } from "../port/mock_hub";
import { boot, STORAGE } from "../i18n";
import { refresh } from "./viewport";

// jsdom lays nothing out, so the width the fold reads is whatever is said here.
function room(width: number) {
  vi.spyOn(document.body, "clientWidth", "get").mockReturnValue(width);
  vi.spyOn(document.body, "clientHeight", "get").mockReturnValue(900);
  refresh();
}

beforeEach(async () => {
  await import("./Settings");
  sessionStorage.clear();
  localStorage.clear();
  localStorage.setItem(STORAGE, "zh");
  boot();
  vi.spyOn(HTMLCanvasElement.prototype, "getContext").mockReturnValue(null);
  room(1440);
});
afterEach(() => {
  cleanup();
  sessionStorage.clear();
  localStorage.clear();
  delete document.documentElement.dataset.fold;
  vi.restoreAllMocks();
});

async function open() {
  const hub = new MockHub();
  const build = hub.portFor.bind(hub);
  const prepared = new WeakSet();
  hub.portFor = (rt) => {
    const port = build(rt);
    if (!prepared.has(port)) {
      prepared.add(port);
      port.providerSetup = async () => null;
      port.welcomeSeen = async () => true;
    }
    return port;
  };
  const view = render(<App hub={hub} />);
  await screen.findByRole("combobox", { name: "任务输入" });
  return { hub, view };
}


const more = () => screen.queryByRole("button", { name: "更多" });
const fold = () => document.querySelector<HTMLElement>(".chrome .r-fold")!;

it("keeps every title-bar control in the bar while there is room, with no 'more' button", async () => {
  await open();
  expect(more()).toBeNull();
  expect(document.querySelector(".chrome .theme-toggle")).not.toBeNull();
  expect(document.documentElement.dataset.fold ?? "").not.toContain("bar");
});

it("gathers the controls under one button below the bar fold, keeps them in the document, and closes on Escape", async () => {
  room(300);
  await open();
  expect(document.documentElement.dataset.fold).toContain("bar");
  const button = more()!;
  expect(button.getAttribute("aria-expanded")).toBe("false");
  for (const sel of [".find-action", ".theme-toggle", ".view-action"]) {
    expect(document.querySelector(`.chrome .r-fold ${sel}`), sel).not.toBeNull();
  }
  await userEvent.click(button);
  expect(button.getAttribute("aria-expanded")).toBe("true");
  expect(fold().hasAttribute("data-open")).toBe(true);
  expect(button.getAttribute("aria-haspopup")).toBe("true");
  expect(document.getElementById(button.getAttribute("aria-controls")!)).toBe(fold());
  document.querySelector<HTMLElement>(".chrome .find-action")!.focus();
  await userEvent.keyboard("{Escape}");
  expect(fold().hasAttribute("data-open")).toBe(false);
  expect(document.activeElement).toBe(button);
});

it("a control inside the fold still does its job", async () => {
  room(300);
  await open();
  const before = document.documentElement.dataset.theme;
  await userEvent.click(more()!);
  await userEvent.click(document.querySelector<HTMLElement>(".chrome .theme-toggle")!);
  expect(document.documentElement.dataset.theme).not.toBe(before);
});

it("unfolds again when the room comes back", async () => {
  room(300);
  await open();
  expect(more()).not.toBeNull();
  act(() => room(900));
  expect(more()).toBeNull();
});
