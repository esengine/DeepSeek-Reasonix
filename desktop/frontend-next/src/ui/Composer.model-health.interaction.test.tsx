// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import "./testkit";
import { Composer } from "./Composer";
import { MockPort } from "../port/mock";
import type { SessionStatus } from "../port/port";
import { t } from "../i18n";

afterEach(() => { cleanup(); localStorage.clear(); vi.restoreAllMocks(); });

it("keeps account headings, visible hosts and row metadata with checks off and on", async () => {
  const port = new MockPort();
  vi.spyOn(port, "models").mockResolvedValue([
    { ref: "a/current", provider: "a", model: "current", kind: "openai", displayName: "Gateway", vendor: "one.example" },
    { ref: "a/second", provider: "a", model: "second", kind: "openai", displayName: "Gateway", vendor: "one.example" },
    { ref: "b/third", provider: "b", model: "third", kind: "openai", displayName: "Gateway", vendor: "two.example" },
  ]);
  const probe = vi.spyOn(port, "providerHealth").mockResolvedValue({ connection: "reachable", wallet: "available", checkedAt: 1 });
  const props = { port, status: { modelRef: "a/current" } as SessionStatus, running: false, onSubmit: async () => true as const, onChanged: vi.fn(), onError: vi.fn() };
  const view = render(<Composer {...props} pulse={0} />);
  fireEvent.click(await screen.findByRole("button", { name: "current" }));
  const menu = screen.getByRole("menu");
  expect(menu.classList.contains("compact-model-menu")).toBe(false);
  expect(within(menu).getByText("one.example")).toBeTruthy();
  expect(within(menu).getByText("two.example")).toBeTruthy();
  expect(within(menu).getAllByRole("menuitem")).toHaveLength(4);
  expect(probe).not.toHaveBeenCalled();
  fireEvent.keyDown(menu, { key: "Escape" });
  await port.saveProviderHealthSettings(true);
  view.rerender(<Composer {...props} pulse={1} />);
  fireEvent.click(screen.getByRole("button", { name: "current" }));
  await within(screen.getByRole("menu")).findAllByText(t("连接正常"));
  expect(screen.getByRole("menu").querySelectorAll(".mi.head .rt")).toHaveLength(2);
  expect(screen.getByRole("menuitem", { name: /second/ }).textContent).toContain(t("OpenAI 兼容"));
  expect(screen.getByRole("menuitem", { name: /second/ }).textContent).not.toContain("Gateway");
});

it("checks on opening, shows wallet failures and keeps selection and management usable", async () => {
  const port = new MockPort();
  await port.saveProviderHealthSettings(true);
  vi.spyOn(port, "models").mockResolvedValue([
    { ref: "one/alpha", provider: "one", model: "alpha", kind: "openai" },
    { ref: "one/beta", provider: "one", model: "beta", kind: "openai" },
    { ref: "two/gamma", provider: "two", model: "gamma", kind: "openai" },
  ]);
  const check = vi.spyOn(port, "providerHealth").mockImplementation(async (name) => ({ connection: "reachable", wallet: name === "one" ? "insufficient" : "unsupported", checkedAt: Date.now() }));
  const trial = vi.spyOn(port, "checkProviderModel");
  const pick = vi.spyOn(port, "setModel").mockResolvedValue();
  const settings = vi.fn();
  render(<Composer port={port} status={{ modelRef: "one/alpha" } as SessionStatus} running={false} onSubmit={async () => true} onChanged={vi.fn()} onError={vi.fn()} onSettings={settings} />);
  const trigger = await screen.findByRole("button", { name: "alpha" });
  expect(check).not.toHaveBeenCalled();
  fireEvent.click(trigger);
  await screen.findAllByText(/Insufficient balance|余额不足/);
  expect(check.mock.calls.map(([name]) => name).sort()).toEqual(["one", "two"]);
  expect(trial).not.toHaveBeenCalled();
  expect(screen.getByRole("menuitem", { name: /alpha/ }).hasAttribute("data-on")).toBe(true);
  fireEvent.click(screen.getByRole("menuitem", { name: /gamma/ }));
  await waitFor(() => expect(pick).toHaveBeenCalledWith("two/gamma"));
  fireEvent.click(trigger);
  fireEvent.click(screen.getByRole("menuitem", { name: /Manage models and connections|管理模型与连接/ }));
  expect(settings).toHaveBeenCalledWith("providers");
});

it("ignores a late health result after switching runtime", async () => {
  const oldPort = new MockPort(), port = new MockPort();
  await oldPort.saveProviderHealthSettings(true);
  await port.saveProviderHealthSettings(true);
  const models = [{ ref: "one/alpha", provider: "one", model: "alpha" }];
  vi.spyOn(oldPort, "models").mockResolvedValue(models);
  vi.spyOn(port, "models").mockResolvedValue(models);
  let finish!: (value: { connection: string; wallet: string; checkedAt: number }) => void;
  vi.spyOn(oldPort, "providerHealth").mockReturnValue(new Promise((resolve) => { finish = resolve; }));
  vi.spyOn(port, "providerHealth").mockResolvedValue({ connection: "reachable", wallet: "available", checkedAt: Date.now() });
  const props = { status: { modelRef: "one/alpha" } as SessionStatus, running: false, onSubmit: async () => true as const, onChanged: vi.fn(), onError: vi.fn() };
  const view = render(<Composer {...props} port={oldPort} />);
  fireEvent.click(await screen.findByRole("button", { name: "alpha" }));
  await waitFor(() => expect(finish).toBeDefined());
  view.rerender(<Composer {...props} port={port} />);
  await act(async () => { finish({ connection: "auth", wallet: "insufficient", checkedAt: Date.now() }); });
  expect(screen.queryByText(/Insufficient balance|余额不足/)).toBeNull();
});

it("limits background checks, tolerates older servers and excludes extension models", async () => {
  const port = new MockPort();
  await port.saveProviderHealthSettings(true);
  vi.spyOn(port, "models").mockResolvedValue([
    ...["one", "two", "three", "four"].map((name) => ({ ref: `${name}/alpha`, provider: name, model: "alpha" })),
    { ref: "plugin/test/provider/alpha", provider: "plugin/test/provider", model: "extension", kind: "extension" },
  ]);
  const pending: Array<() => void> = [];
  const check = vi.spyOn(port, "providerHealth").mockImplementation(() => new Promise((_, reject) => { pending.push(() => reject(new Error("old server"))); }));
  render(<Composer port={port} status={{ modelRef: "one/alpha" } as SessionStatus} running={false} onSubmit={async () => true} onChanged={vi.fn()} onError={vi.fn()} />);
  fireEvent.click(await screen.findByRole("button", { name: "alpha" }));
  await waitFor(() => expect(check).toHaveBeenCalledTimes(3));
  await act(async () => { pending[0](); });
  await waitFor(() => expect(check).toHaveBeenCalledTimes(4));
  await act(async () => { pending.slice(1).forEach((finish) => finish()); });
  expect(check.mock.calls.flat()).not.toContain("plugin/test/provider");
  expect(screen.getByRole("menuitem", { name: /extension/ })).toBeTruthy();
  expect(screen.getAllByText(/Unconfirmed|连接待确认/)).toHaveLength(4);
});
