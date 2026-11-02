// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, renderHook, screen, waitFor } from "@testing-library/react";
import "./testkit";
import { Appearance } from "./Appearance";
import { usePaint } from "./paint";
import { MockHub } from "../port/mock_hub";
import { boot, STORAGE } from "../i18n";

beforeEach(() => {
  localStorage.setItem(STORAGE, "zh");
  boot();
  vi.spyOn(HTMLCanvasElement.prototype, "getContext").mockReturnValue(null);
});
afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  document.documentElement.removeAttribute("style");
  delete document.documentElement.dataset.measure;
});

async function open() {
  const hub = new MockHub();
  const runtimes = await hub.runtimes();
  const port = hub.machinePort();
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

const slider = () => screen.getByRole("slider", { name: "对话区域宽度" }) as HTMLInputElement;

describe("custom conversation width", () => {
  it("adjusts the percentage without changing zoom or text size, and retains it across presets", async () => {
    const { paint, again } = await open();
    fireEvent.click(screen.getByRole("button", { name: "自定义比例" }));
    await waitFor(() => expect(paint.result.current.look.width).toBe("custom"));
    again();
    fireEvent.change(slider(), { target: { value: "67" } });
    await waitFor(() => expect(paint.result.current.look.widthPercent).toBe(67));
    again();
    expect(slider().value).toBe("67");
    expect(paint.result.current.look.zoom).toBeUndefined();
    expect(paint.result.current.look.readSize).toBeUndefined();
    fireEvent.click(screen.getByRole("button", { name: "铺满窗口" }));
    await waitFor(() => expect(paint.result.current.look.width).toBe("full"));
    again();
    expect(screen.queryByRole("slider", { name: "对话区域宽度" })).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "自定义比例" }));
    await waitFor(() => expect(paint.result.current.look.width).toBe("custom"));
    again();
    expect(slider().value).toBe("67");
    fireEvent.click(screen.getByRole("button", { name: "宽松" }));
    await waitFor(() => expect(paint.result.current.look.zoom).toBe(1.15));
    again();
    expect(slider().value).toBe("67");
    expect(paint.result.current.look.width).toBe("custom");
  });
});
