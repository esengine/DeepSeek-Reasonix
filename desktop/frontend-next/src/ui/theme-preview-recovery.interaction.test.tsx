// @vitest-environment jsdom
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { Appearance } from "./Appearance";
import { MockPort } from "../port/mock";
import { SseTheme } from "../port/sse_theme";
import { boot, STORAGE } from "../i18n";
import type { AgentPort, ThemePack } from "../port/port";

const dusk: ThemePack = {
  id: "dusk", name: "Dusk", hasPreview: true,
  tokens: { light: { bg: "#F6F3EE" }, dark: { bg: "#0F0D0B" } },
};

beforeEach(() => {
  localStorage.setItem(STORAGE, "zh");
  boot();
  vi.spyOn(HTMLCanvasElement.prototype, "getContext").mockReturnValue(null);
});

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

function picker(port: AgentPort, theme = "light") {
  return <Appearance port={port} theme={theme} onTheme={() => {}} contrast="" onContrast={() => {}}
    weight="" onWeight={() => {}} look={{}} onLook={() => {}} reloadThemes={() => {}} />;
}

function inventory() {
  const port = new MockPort() as unknown as AgentPort;
  const fetch = vi.fn(async () => Response.json([dusk]));
  vi.stubGlobal("fetch", fetch);
  const transport = new SseTheme();
  const themes = vi.spyOn(port, "themes").mockImplementation(() => transport.themes());
  vi.spyOn(port, "activateTheme").mockResolvedValue();
  vi.spyOn(port, "importTheme").mockResolvedValue({ pack: dusk });
  return { port, themes, fetch };
}

it.each(["import", "activation"])("retries a failed preview after a successful %s refresh without remounting its card", async (action) => {
  const { port, themes, fetch } = inventory();
  const view = render(picker(port));
  const card = await screen.findByRole("button", { name: /Dusk/ });
  const preview = card.querySelector("img")!;
  expect(preview.getAttribute("src")).toBe("/themes/dusk/preview");
  fireEvent.error(preview);
  expect(card.querySelector("img")).toBeNull();
  expect((card.querySelector(".pal-art") as HTMLElement).style.getPropertyValue("--pal-page")).toBe("#F6F3EE");
  view.rerender(picker(port, "dark"));
  expect(card.querySelector("img")).toBeNull();
  expect(themes).toHaveBeenCalledTimes(1);
  if (action === "import") {
    fireEvent.change(document.querySelector('input[data-action="theme.import"]')!, {
      target: { files: [new File(["fixture"], "dusk.zip")] },
    });
  } else {
    await userEvent.click(card);
  }
  await waitFor(() => expect(themes).toHaveBeenCalledTimes(2));
  await waitFor(() => expect(card.querySelector("img")).not.toBeNull());
  expect(screen.getByRole("button", { name: /Dusk/ })).toBe(card);
  expect(card.querySelector("img")!.getAttribute("src")).toBe("/themes/dusk/preview");
  expect(fetch).toHaveBeenCalledTimes(2);
  expect(fetch).toHaveBeenLastCalledWith("/themes", { credentials: "same-origin" });
  fireEvent.error(card.querySelector("img")!);
  expect(card.querySelector("img")).toBeNull();
  view.rerender(picker(port));
  expect(card.querySelector("img")).toBeNull();
  expect(themes).toHaveBeenCalledTimes(2);
});

it("keeps a working preview mounted across ordinary palette redraws", async () => {
  const { port, themes } = inventory();
  const view = render(picker(port));
  const card = await screen.findByRole("button", { name: /Dusk/ });
  const preview = card.querySelector("img");
  view.rerender(picker(port, "dark"));
  expect(card.querySelector("img")).toBe(preview);
  expect((card.querySelector(".pal-art") as HTMLElement).style.getPropertyValue("--pal-page")).toBe("#0F0D0B");
  expect(themes).toHaveBeenCalledTimes(1);
});

it.each(["zh", "en"])("keeps foundations visible and previews their own scheme in %s", async (lang) => {
  localStorage.setItem(STORAGE, lang);
  boot();
  const port = new MockPort();
  const view = render(picker(port));
  const blue = await screen.findByRole("button", { name: lang === "zh" ? /清白蓝/ : /Clear Blue/ });
  const green = screen.getByRole("button", { name: lang === "zh" ? /茶绿/ : /Tea Green/ });
  expect(blue.closest("details")).toBeNull();
  expect(green.closest("details")).toBeNull();
  const sky = document.querySelector('.pal[data-value="tide"]')!;
  expect(sky.closest("details")?.open).toBe(true);
  const art = blue.querySelector(".pal-art") as HTMLElement;
  expect(art.style.getPropertyValue("--pal-page")).toBe("#F5F7FA");
  await userEvent.click(green);
  await waitFor(() => expect(green.getAttribute("aria-pressed")).toBe("true"));
  expect(art.style.getPropertyValue("--pal-page")).toBe("#F5F7FA");
  view.rerender(picker(port, "dark"));
  expect(art.style.getPropertyValue("--pal-page")).toBe("#151B24");
  expect((green.querySelector(".pal-art") as HTMLElement).style.getPropertyValue("--pal-page")).toBe("#18211B");
});
