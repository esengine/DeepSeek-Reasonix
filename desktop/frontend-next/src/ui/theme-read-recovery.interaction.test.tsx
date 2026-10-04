// @vitest-environment jsdom
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, renderHook, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { Appearance } from "./Appearance";
import { usePaint } from "./paint";
import { MockPort } from "../port/mock";
import { MockHub } from "../port/mock_hub";
import { boot, STORAGE, t } from "../i18n";
import type { AgentPort, ThemePack } from "../port/port";

const dusk: ThemePack = { id: "dusk", name: "Dusk", active: true, tokens: { light: { bg: "#F6F3EE" } } };
const unread = new Error("theme inventory unavailable");

beforeEach(() => {
  localStorage.setItem(STORAGE, "zh");
  localStorage.setItem("rx-theme", "light");
  boot();
  vi.spyOn(HTMLCanvasElement.prototype, "getContext").mockReturnValue(null);
});

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  document.documentElement.removeAttribute("style");
});

function picker(port: AgentPort) {
  return <Appearance port={port} theme="light" onTheme={() => {}} contrast="" onContrast={() => {}}
    weight="" onWeight={() => {}} look={{}} onLook={() => {}} reloadThemes={() => {}} />;
}

it("keeps installed choices when their refresh cannot be read and recovers on the next selection", async () => {
  const port = new MockPort() as unknown as AgentPort;
  const themes = vi.spyOn(port, "themes").mockResolvedValueOnce([dusk]).mockRejectedValueOnce(unread).mockResolvedValue([]);
  vi.spyOn(port, "activateTheme").mockResolvedValue();
  render(picker(port));
  await screen.findByRole("button", { name: /Dusk/ });
  await userEvent.click(screen.getByRole("button", { name: /Dusk/ }));
  expect(screen.getByRole("button", { name: /Dusk/ })).toBeTruthy();
  expect((await screen.findByRole("alert")).textContent).toBe(unread.message);
  expect(screen.queryByText(t("尚未安装主题。选择一个 .zip，或同时选中主题文件夹里的 theme.json 和图片。"))).toBeNull();
  await userEvent.click(screen.getByRole("button", { name: /默认/ }));
  await waitFor(() => expect(themes).toHaveBeenCalledTimes(3));
  await waitFor(() => expect(screen.queryByRole("button", { name: /Dusk/ })).toBeNull());
  expect(screen.queryByRole("alert")).toBeNull();
  expect(screen.getByRole("button", { name: /默认/ }).getAttribute("aria-pressed")).toBe("true");
});

it("reports an initial theme inventory read failure", async () => {
  const port = new MockPort() as unknown as AgentPort;
  vi.spyOn(port, "themes").mockRejectedValue(unread);
  render(picker(port));
  expect((await screen.findByRole("alert")).textContent).toBe(unread.message);
});

it("clears a read failure when an import refresh successfully loads the installed packs", async () => {
  const port = new MockPort() as unknown as AgentPort;
  vi.spyOn(port, "themes").mockRejectedValueOnce(unread).mockResolvedValue([dusk]);
  vi.spyOn(port, "importTheme").mockResolvedValue({ pack: dusk });
  render(picker(port));
  await screen.findByRole("alert");
  fireEvent.change(document.querySelector('input[data-action="theme.import"]')!, {
    target: { files: [new File(["fixture"], "dusk.zip")] },
  });
  await screen.findByRole("button", { name: /Dusk/ });
  expect(screen.queryByRole("alert")).toBeNull();
});

it("keeps the painted pack after a failed read and clears it after a successful default selection", async () => {
  const hub = new MockHub();
  const runtimes = await hub.runtimes();
  const port = hub.portFor(runtimes[0]);
  vi.spyOn(port, "themes").mockResolvedValueOnce([dusk]).mockRejectedValueOnce(unread).mockResolvedValue([]);
  const onError = vi.fn();
  const { result } = renderHook(() => usePaint(hub, runtimes, false, onError));
  await waitFor(() => expect(document.documentElement.style.getPropertyValue("--page")).toBe("#F6F3EE"));
  await act(async () => result.current.reloadThemes());
  expect(result.current.pack?.id).toBe("dusk");
  expect(document.documentElement.style.getPropertyValue("--page")).toBe("#F6F3EE");
  expect(onError).toHaveBeenCalledExactlyOnceWith(unread);
  await act(async () => result.current.reloadThemes());
  expect(result.current.pack).toBeNull();
  expect(document.documentElement.style.getPropertyValue("--page")).toBe("");
  expect(onError).toHaveBeenCalledTimes(1);
});

it("reports an initial window theme read failure without inventing an active pack", async () => {
  const hub = new MockHub();
  const runtimes = await hub.runtimes();
  vi.spyOn(hub.portFor(runtimes[0]), "themes").mockRejectedValue(unread);
  const onError = vi.fn();
  const { result } = renderHook(() => usePaint(hub, runtimes, false, onError));
  await waitFor(() => expect(onError).toHaveBeenCalledExactlyOnceWith(unread));
  expect(result.current.pack).toBeNull();
});
