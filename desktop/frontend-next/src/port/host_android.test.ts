import { afterEach, expect, it, vi } from "vitest";

afterEach(() => { vi.unstubAllGlobals(); vi.resetModules(); });

it("recognizes Android at startup and waits for its asynchronous picker bridge", async () => {
  const pickFolder = vi.fn(async () => "/storage/emulated/0/Documents/Reasonix");
  const openExternal = vi.fn();
  const blockingPicker = vi.fn(() => { throw new Error("synchronous picker must not run"); });
  const win = Object.assign(new EventTarget(), { ReasonixNative: { pickFolder: blockingPicker, openExternal }, reasonixHost: undefined as unknown });
  vi.stubGlobal("window", win);
  const { host } = await import("./host");
  expect(await host().describe()).toEqual({ shell: "electron", platform: "android", titleBar: false });
  const selection = host().pickFolder("/storage/emulated/0/Documents");
  win.reasonixHost = { pickFolder };
  win.dispatchEvent(new Event("reasonix-host-ready"));
  expect(await selection).toBe("/storage/emulated/0/Documents/Reasonix");
  expect(blockingPicker).not.toHaveBeenCalled();
  expect(pickFolder).toHaveBeenCalledWith("/storage/emulated/0/Documents");
  await host().openExternal("https://example.com");
  expect(openExternal).toHaveBeenCalledWith("https://example.com");
});

it("keeps Android picker cancellation distinct from an unsupported browser picker", async () => {
  vi.stubGlobal("window", { ReasonixNative: { openExternal: vi.fn() }, reasonixHost: { pickFolder: async () => "" } });
  const { host } = await import("./host");
  expect(await host().pickFolder("")).toBe("");
  expect(host().inShell()).toBe(true);
});

it("synchronizes Android system bars with the page theme after the bridge is ready", async () => {
  const syncTheme = vi.fn();
  const win = Object.assign(new EventTarget(), { ReasonixNative: {}, reasonixHost: undefined as unknown });
  vi.stubGlobal("window", win);
  const { host } = await import("./host");
  host().syncTheme("dark", "dark");
  win.reasonixHost = { syncTheme };
  win.dispatchEvent(new Event("reasonix-host-ready"));
  await vi.waitFor(() => expect(syncTheme).toHaveBeenCalledWith("dark", "dark"));
  host().syncTheme("light", "light");
  await vi.waitFor(() => expect(syncTheme).toHaveBeenCalledWith("light", "light"));
});

it("returns a cancelled scan without navigating and can scan again", async () => {
  const openInApp = vi.fn();
  const scanPairingCode = vi.fn().mockResolvedValueOnce("").mockResolvedValueOnce("http://192.168.1.2:4321/#pair=test");
  vi.stubGlobal("window", { ReasonixNative: { openInApp }, reasonixHost: { scanPairingCode } });
  const { host } = await import("./host");
  expect(host().canScanPairingCode()).toBe(true);
  expect(await host().scanPairingCode()).toBe("");
  expect(openInApp).not.toHaveBeenCalled();
  expect(await host().scanPairingCode()).toBe("http://192.168.1.2:4321/#pair=test");
  host().openInApp("http://192.168.1.2:4321/#pair=test");
  expect(openInApp).toHaveBeenCalledOnce();
});

it("uses browser views attached after Android startup without exposing desktop window controls", async () => {
  const win: { ReasonixNative: unknown; reasonixHost?: unknown } = { ReasonixNative: { pickFolder: () => "", openExternal: vi.fn() } };
  vi.stubGlobal("window", win);
  const { host } = await import("./host");
  const shell = host();
  expect(shell.drawsBrowserViews()).toBe(false);
  const showBrowserView = vi.fn();
  const navigateBrowserView = vi.fn(async () => "");
  win.reasonixHost = { showBrowserView, navigateBrowserView };
  expect(shell.drawsBrowserViews()).toBe(true);
  const rect = { x: 0, y: 0, width: 320, height: 480 };
  shell.showBrowserView("page", rect);
  expect(showBrowserView).toHaveBeenCalledWith("page", rect);
  expect(await shell.navigateBrowserView("page", "https://example.com")).toBe("");
  expect(navigateBrowserView).toHaveBeenCalledWith("page", "https://example.com");
  expect(await shell.isWindowMaximised()).toBe(false);
  expect(await shell.graphics()).toBeNull();
  expect(() => shell.minimiseWindow()).not.toThrow();
});
