// @vitest-environment jsdom
import { act, cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import "./testkit";
import { App } from "./App";
import { MockHub } from "../port/mock_hub";
import { boot, STORAGE } from "../i18n";
import { refresh } from "./viewport";
import { resetUpdateWatch, startUpdateWatch } from "../state/updatewatch";

beforeEach(() => {
  sessionStorage.clear();
  localStorage.clear();
  localStorage.setItem("rx-nav-rail", "off");
  localStorage.setItem(STORAGE, "zh");
  boot();
  vi.spyOn(document.body, "clientWidth", "get").mockReturnValue(1440);
  vi.spyOn(document.body, "clientHeight", "get").mockReturnValue(900);
  vi.spyOn(HTMLCanvasElement.prototype, "getContext").mockReturnValue(null);
  refresh();
});
afterEach(() => { cleanup(); resetUpdateWatch(); vi.useRealTimers(); vi.restoreAllMocks(); localStorage.clear(); sessionStorage.clear(); });

async function newerRelease() {
  vi.useFakeTimers();
  const hub = { current: "2.57.0", pinned: "", stalePin: false, latest: "2.58.0", newer: true, versions: [] };
  const stop = startUpdateWatch({ updateMode: () => Promise.resolve("notify" as const), versions: () => Promise.resolve(hub) });
  await act(async () => { await vi.advanceTimersByTimeAsync(61_000); });
  vi.useRealTimers();
  return stop;
}

async function mount() {
  const hub = new MockHub();
  const runtime = await hub.open({ root: "~/projects/my-website", sessionPath: "/sessions/site.jsonl" });
  for (const rt of await hub.runtimes()) {
    const port = hub.portFor(rt);
    port.providerSetup = async () => null;
    port.welcomeSeen = async () => true;
  }
  sessionStorage.setItem("rx-active-pane", runtime.id);
  await act(async () => { render(<App hub={hub} />); });
  await waitFor(() => expect(document.querySelector(".railfoot")).toBeTruthy());
}

it("offers a newer release in the sidebar footer and opens the version page from it", async () => {
  const stop = await newerRelease();
  await mount();
  const notice = await screen.findByRole("button", { name: /2\.58\.0/ });
  expect(notice.closest(".railfoot")).not.toBeNull();
  await userEvent.click(notice);
  await waitFor(() => expect(document.querySelector(".app")!.hasAttribute("data-prefs")).toBe(true));
  const tabs = await screen.findAllByRole("tab");
  expect(tabs.find((el) => el.getAttribute("aria-selected") === "true")?.getAttribute("data-value")).toBe("account");
  expect(await screen.findByText(/2\.58\.0/, { selector: ".prefs-sheet *" })).toBeTruthy();
  stop();
});

it("shows nothing in the footer while no newer release is known", async () => {
  await mount();
  expect(document.querySelector('.railfoot [data-action="update.open"]')).toBeNull();
});
