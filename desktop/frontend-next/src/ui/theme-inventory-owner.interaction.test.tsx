// @vitest-environment jsdom
import { StrictMode } from "react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { Appearance } from "./Appearance";
import { MockPort } from "../port/mock";
import { boot, STORAGE } from "../i18n";
import type { AgentPort, ThemePack } from "../port/port";

const pack = (id: string): ThemePack => ({ id, name: id, tokens: {}, active: true });
function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (error: Error) => void;
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
}
const port = () => new MockPort() as unknown as AgentPort;
function picker(connection: AgentPort) {
  return <Appearance port={connection} theme="light" onTheme={() => {}} contrast="" onContrast={() => {}}
    weight="" onWeight={() => {}} look={{}} onLook={() => {}} reloadThemes={() => {}} />;
}
beforeEach(() => {
  localStorage.setItem(STORAGE, "zh");
  boot();
  vi.spyOn(HTMLCanvasElement.prototype, "getContext").mockReturnValue(null);
});
afterEach(() => { cleanup(); vi.restoreAllMocks(); });

it.each(["packs", "error"])("clears settled %s while a new connection's inventory is pending", async (state) => {
  const first = port(), second = port(), pending = deferred<ThemePack[]>();
  vi.spyOn(first, "themes").mockImplementation(() => state === "packs"
    ? Promise.resolve([{ ...pack("Remote pack"), warnings: ["Remote warning"] }])
    : Promise.reject(new Error("Remote inventory failed")));
  vi.spyOn(second, "themes").mockReturnValue(pending.promise);
  const view = render(picker(first));
  if (state === "packs") await screen.findByRole("button", { name: /Remote pack/ });
  else await screen.findByRole("alert");
  view.rerender(picker(second));
  expect(screen.queryByRole("button", { name: /Remote pack/ })).toBeNull();
  expect(screen.queryByText("Remote warning")).toBeNull();
  expect(screen.queryByRole("alert")).toBeNull();
  await act(async () => pending.resolve([pack("Local pack")]));
  expect(screen.getByRole("button", { name: /Local pack/ })).toBeTruthy();
});

it.each(["success", "failure"])("ignores an old connection's late inventory %s", async (outcome) => {
  const first = port(), second = port(), old = deferred<ThemePack[]>();
  vi.spyOn(first, "themes").mockReturnValue(old.promise);
  vi.spyOn(second, "themes").mockResolvedValue([pack("Local pack")]);
  const view = render(picker(first));
  view.rerender(picker(second));
  await screen.findByRole("button", { name: /Local pack/ });
  await act(async () => outcome === "success" ? old.resolve([pack("Remote pack")]) : old.reject(new Error("Old read failed")));
  expect(screen.getByRole("button", { name: /Local pack/ })).toBeTruthy();
  expect(screen.queryByRole("button", { name: /Remote pack/ })).toBeNull();
  expect(screen.queryByRole("alert")).toBeNull();
});

it.each(["success", "failure"])("invalidates an earlier visit after A to B to A on late %s", async (outcome) => {
  const first = port(), second = port(), old = deferred<ThemePack[]>();
  vi.spyOn(first, "themes").mockReturnValueOnce(old.promise).mockResolvedValue([pack("Fresh A")]);
  vi.spyOn(second, "themes").mockResolvedValue([pack("B pack")]);
  const view = render(picker(first));
  view.rerender(picker(second));
  await screen.findByRole("button", { name: /B pack/ });
  view.rerender(picker(first));
  await screen.findByRole("button", { name: /Fresh A/ });
  await act(async () => outcome === "success" ? old.resolve([pack("Old A")]) : old.reject(new Error("Old A failed")));
  expect(screen.getByRole("button", { name: /Fresh A/ })).toBeTruthy();
  expect(screen.queryByRole("button", { name: /Old A/ })).toBeNull();
  expect(screen.queryByRole("alert")).toBeNull();
});

it.each(["success", "failure"])("keeps the latest same-connection refresh after an earlier %s", async (outcome) => {
  const connection = port(), old = deferred<ThemePack[]>();
  vi.spyOn(connection, "themes").mockResolvedValueOnce([pack("Initial pack")])
    .mockReturnValueOnce(old.promise).mockResolvedValue([pack("Latest pack")]);
  vi.spyOn(connection, "activateTheme").mockResolvedValue();
  render(picker(connection));
  await screen.findByRole("button", { name: /Initial pack/ });
  await userEvent.click(screen.getByRole("button", { name: /默认/ }));
  await userEvent.click(screen.getByRole("button", { name: /默认/ }));
  await screen.findByRole("button", { name: /Latest pack/ });
  await act(async () => outcome === "success" ? old.resolve([pack("Outdated pack")]) : old.reject(new Error("Outdated read failed")));
  expect(screen.getByRole("button", { name: /Latest pack/ })).toBeTruthy();
  expect(screen.queryByRole("button", { name: /Outdated pack/ })).toBeNull();
  expect(screen.queryByRole("alert")).toBeNull();
});

it("keeps the newest read failure and known-good list when an older read succeeds", async () => {
  const connection = port(), old = deferred<ThemePack[]>();
  vi.spyOn(connection, "themes").mockResolvedValueOnce([pack("Known pack")])
    .mockReturnValueOnce(old.promise).mockRejectedValue(new Error("Newest read failed"));
  vi.spyOn(connection, "activateTheme").mockResolvedValue();
  render(picker(connection));
  await screen.findByRole("button", { name: /Known pack/ });
  await userEvent.click(screen.getByRole("button", { name: /默认/ }));
  await userEvent.click(screen.getByRole("button", { name: /默认/ }));
  await screen.findByRole("alert");
  await act(async () => old.resolve([pack("Outdated pack")]));
  expect(screen.getByRole("alert").textContent).toBe("Newest read failed");
  expect(screen.getByRole("button", { name: /Known pack/ })).toBeTruthy();
});

it.each(["activation", "import"])("does not start an old %s refresh after changing connections", async (action) => {
  const first = port(), second = port(), old = deferred<void>();
  const read = vi.spyOn(first, "themes").mockResolvedValue([pack("Remote pack")]);
  vi.spyOn(second, "themes").mockResolvedValue([pack("Local pack")]);
  vi.spyOn(first, "activateTheme").mockReturnValue(old.promise);
  vi.spyOn(first, "importTheme").mockImplementation(async () => { await old.promise; return { pack: pack("Imported pack") }; });
  const view = render(picker(first));
  await screen.findByRole("button", { name: /Remote pack/ });
  if (action === "activation") await userEvent.click(screen.getByRole("button", { name: /Remote pack/ }));
  else fireEvent.change(document.querySelector('input[data-action="theme.import"]')!, { target: { files: [new File(["fixture"], "theme.zip")] } });
  view.rerender(picker(second));
  await screen.findByRole("button", { name: /Local pack/ });
  await act(async () => old.resolve());
  expect(read).toHaveBeenCalledTimes(1);
  expect(screen.getByRole("button", { name: /Local pack/ })).toBeTruthy();
});

it.each(["activation", "import"])("does not start an %s refresh after Appearance unmounts", async (action) => {
  const connection = port(), old = deferred<void>();
  const read = vi.spyOn(connection, "themes").mockResolvedValue([pack("Installed pack")]);
  vi.spyOn(connection, "activateTheme").mockReturnValue(old.promise);
  vi.spyOn(connection, "importTheme").mockImplementation(async () => { await old.promise; return { pack: pack("Imported pack") }; });
  const view = render(picker(connection));
  await screen.findByRole("button", { name: /Installed pack/ });
  if (action === "activation") await userEvent.click(screen.getByRole("button", { name: /Installed pack/ }));
  else fireEvent.change(document.querySelector('input[data-action="theme.import"]')!, { target: { files: [new File(["fixture"], "theme.zip")] } });
  view.unmount();
  await act(async () => old.resolve());
  expect(read).toHaveBeenCalledTimes(1);
});

it("invalidates the first inventory request during StrictMode effect replay", async () => {
  const connection = port(), old = deferred<ThemePack[]>();
  const read = vi.spyOn(connection, "themes").mockReturnValueOnce(old.promise).mockResolvedValue([pack("Current pack")]);
  render(<StrictMode>{picker(connection)}</StrictMode>);
  await screen.findByRole("button", { name: /Current pack/ });
  expect(read).toHaveBeenCalledTimes(2);
  await act(async () => old.resolve([pack("Old pack")]));
  expect(screen.getByRole("button", { name: /Current pack/ })).toBeTruthy();
});
