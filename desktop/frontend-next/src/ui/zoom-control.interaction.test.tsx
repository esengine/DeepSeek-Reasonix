// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, renderHook, screen, waitFor } from "@testing-library/react";
import "./testkit";
import { Appearance } from "./Appearance";
import { usePaint } from "./paint";
import { MockHub } from "../port/mock_hub";
import { boot, STORAGE } from "../i18n";
import { host } from "../port/host";

beforeEach(() => {
  localStorage.setItem(STORAGE, "zh");
  boot();
  vi.spyOn(HTMLCanvasElement.prototype, "getContext").mockReturnValue(null);
});
afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  document.documentElement.removeAttribute("style");
});

async function open() {
  const hub = new MockHub();
  const runtimes = await hub.runtimes();
  const port = hub.portFor(runtimes[0]);
  const paint = renderHook(() => usePaint(hub, runtimes, false, () => {}));
  await waitFor(() => expect(paint.result.current.look.zoomRange).toBeDefined());
  const view = () => {
    const p = paint.result.current;
    return <Appearance port={port} theme="light" onTheme={() => {}} contrast="" onContrast={() => {}}
      weight="" onWeight={() => {}} look={p.look} onLook={p.onLook} reloadThemes={() => {}} />;
  };
  const shown = render(view());
  const again = () => shown.rerender(view());
  return { paint, again };
}

const slider = () => screen.getByRole("slider", { name: "界面大小微调" }) as HTMLInputElement;

describe("the interface size control", () => {
  it("draws its slider from the range the kernel announced", async () => {
    const { paint } = await open();
    const range = paint.result.current.look.zoomRange!;
    expect(Number(slider().min)).toBe(range.min);
    expect(Number(slider().max)).toBe(range.max);
    expect(Number(slider().step)).toBe(range.step);
  });

  it("shows what was stored after a drag past the end, not what was dragged", async () => {
    const { paint, again } = await open();
    fireEvent.change(slider(), { target: { value: "2" } });
    await waitFor(() => expect(paint.result.current.look.zoom).toBe(paint.result.current.look.zoomRange!.max));
    again();
    expect(slider().value).toBe("1.8");
    expect(slider().parentElement!.querySelector(".now")!.textContent).toBe("180%");
  });

  it("marks the preset a click chose and leaves the body text size alone", async () => {
    const { paint, again } = await open();
    fireEvent.click(screen.getByRole("button", { name: "宽松" }));
    await waitFor(() => expect(paint.result.current.look.zoom).toBe(1.15));
    again();
    expect(screen.getByRole("button", { name: "宽松" }).getAttribute("aria-pressed")).toBe("true");
    expect(paint.result.current.look.readSize).toBeUndefined();
  });

  it("names the keyboard chords only where they are registered", async () => {
    vi.spyOn(host(), "inShell").mockReturnValue(true);
    await open();
    expect(document.body.textContent).toContain("键盘：");
    cleanup();
    vi.spyOn(host(), "inShell").mockReturnValue(false);
    await open();
    expect(document.body.textContent).not.toContain("键盘：");
  });
});
