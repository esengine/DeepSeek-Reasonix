// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, render, screen } from "@testing-library/react";
import "./testkit";
import { Settings } from "./Settings";
import { MockPort } from "../port/mock";
import { MockHub } from "../port/mock_hub";
import { resetUpdateWatch, startUpdateWatch } from "../state/updatewatch";

afterEach(() => { cleanup(); resetUpdateWatch(); vi.useRealTimers(); });

const settings = (port: MockPort) => (
  <Settings hub={new MockHub()} onError={() => {}} port={port} networkPort={port}
    status={null} theme="" reloadThemes={() => {}} onTheme={() => {}} contrast="" onContrast={() => {}}
    weight="" onWeight={() => {}} look={{}} onLook={() => {}} onClose={() => {}} onChanged={() => {}}
    at="versions" account={null} accountUnread="" reloadAccount={() => {}} />
);

const watch = async (newer: boolean) => {
  vi.useFakeTimers();
  const hub = { current: "2.20.0", pinned: "", stalePin: false, latest: "2.21.0", newer, versions: [] };
  const stop = startUpdateWatch({ updateMode: () => Promise.resolve("notify" as const), versions: () => Promise.resolve(hub) });
  await act(async () => { await vi.advanceTimersByTimeAsync(61_000); });
  vi.useRealTimers();
  return stop;
};

describe("the update mark in the settings list", () => {
  it("sits on the account row, which versions folds into, where the window's gear points", async () => {
    const stop = await watch(true);
    render(settings(new MockPort()));
    const row = (await screen.findAllByRole("tab")).find((el) => el.getAttribute("data-value") === "account");
    expect(row?.querySelector(".upd-dot")).not.toBeNull();
    expect(row?.textContent).toContain("有新版本 2.21.0");
    for (const other of screen.getAllByRole("tab").filter((el) => el !== row)) expect(other.querySelector(".upd-dot")).toBeNull();
    stop();
  });

  it("is absent when no newer release is known", async () => {
    const stop = await watch(false);
    render(settings(new MockPort()));
    await screen.findAllByRole("tab");
    expect(document.querySelector(".prefs-nav .upd-dot")).toBeNull();
    stop();
  });
});
