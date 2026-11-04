// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { act, cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { HttpError, type UpdateMode as Mode } from "../port/port";
import { resetUpdateWatch, startUpdateWatch } from "../state/updatewatch";
import { UpdateDot } from "./UpdateDot";
import { UpdateMode } from "./UpdateMode";

afterEach(() => {
  cleanup();
  resetUpdateWatch();
  vi.useRealTimers();
});

function portAt(mode: Mode, set = vi.fn((m: Mode) => Promise.resolve(m))) {
  return { updateMode: vi.fn(() => Promise.resolve(mode)), setUpdateMode: set } as never;
}

it("shows manual as the choice and says what it does", async () => {
  render(<UpdateMode port={portAt("manual")} />);
  expect((await screen.findByRole("radio", { name: "手动" })).getAttribute("aria-checked")).toBe("true");
  expect(screen.getByRole("radio", { name: "提醒" }).getAttribute("aria-checked")).toBe("false");
  expect(screen.getByText("要不要在后台检查、下载新版本；重启始终由你决定")).toBeTruthy();
});

it("saves notify and says what it does, and nothing downloads", async () => {
  const set = vi.fn((m: Mode) => Promise.resolve(m));
  render(<UpdateMode port={portAt("manual", set)} />);
  await userEvent.click(await screen.findByRole("radio", { name: "提醒" }));
  expect(set).toHaveBeenCalledExactlyOnceWith("notify");
  expect(screen.getByRole("radio", { name: "提醒" }).getAttribute("aria-checked")).toBe("true");
});

it("offers a third choice that downloads but never installs or restarts, off until chosen", async () => {
  const set = vi.fn((m: Mode) => Promise.resolve(m));
  render(<UpdateMode port={portAt("manual", set)} />);
  const choice = await screen.findByRole("radio", { name: "自动下载" });
  expect(choice.getAttribute("aria-checked")).toBe("false");
  await userEvent.click(choice);
  expect(set).toHaveBeenCalledExactlyOnceWith("download");
});

it("keeps the old choice and says why when the kernel refuses", async () => {
  const set = vi.fn(() => Promise.reject(new HttpError(400, "bad", { code: "studio.update_mode_invalid" }, true)));
  render(<UpdateMode port={portAt("manual", set)} />);
  await userEvent.click(await screen.findByRole("radio", { name: "提醒" }));
  expect(await screen.findByText("不支持这种更新方式，没有改动。")).toBeTruthy();
  expect(screen.getByRole("radio", { name: "手动" }).getAttribute("aria-checked")).toBe("true");
});

it("draws nothing where the kernel has no install to update", async () => {
  const port = { updateMode: vi.fn(() => Promise.reject(new Error("no install"))), setUpdateMode: vi.fn() } as never;
  const { container } = render(<UpdateMode port={port} />);
  await act(async () => {});
  expect(container.textContent).toBe("");
});

it("marks the settings entry once a newer release was found, and not before", async () => {
  vi.useFakeTimers();
  render(<UpdateDot />);
  expect(screen.queryByText(/有新版本/)).toBeNull();
  const stop = startUpdateWatch({
    updateMode: () => Promise.resolve("notify" as Mode),
    versions: () => Promise.resolve({ current: "2.20.0", pinned: "", stalePin: false, latest: "2.21.0", newer: true, versions: [] }),
  });
  await act(async () => { await vi.advanceTimersByTimeAsync(70_000); });
  expect(screen.getByText("有新版本 2.21.0，在设置的「版本」里查看")).toBeTruthy();
  stop();
});
