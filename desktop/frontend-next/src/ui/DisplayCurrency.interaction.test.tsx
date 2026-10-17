// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { DisplayCurrency } from "./DisplayCurrency";
import type { AgentPort } from "../port/port";
import type { DisplayCurrencyMode, DisplayCurrencySettings } from "../port/boundary";

afterEach(cleanup);

const base: DisplayCurrencySettings = { mode: "auto", path: "/u/config.toml", unconverted: [] };

function portWith(save: (m: DisplayCurrencyMode) => Promise<DisplayCurrencySettings>) {
  return { displayCurrency: vi.fn(async () => base), saveDisplayCurrency: vi.fn(save) } as unknown as AgentPort & { saveDisplayCurrency: ReturnType<typeof vi.fn> };
}

describe("the display currency setting", () => {
  it("offers three modes, marks the stored one, and writes a change once", async () => {
    const port = portWith(async (mode) => ({ ...base, mode }));
    const changed = vi.fn();
    render(<DisplayCurrency port={port} onChanged={changed} />);
    const auto = await screen.findByRole("button", { name: "自动" });
    expect(auto.getAttribute("aria-pressed")).toBe("true");
    const usd = screen.getByRole("button", { name: "美元 USD" });
    await userEvent.click(usd);
    expect(port.saveDisplayCurrency).toHaveBeenCalledTimes(1);
    expect(port.saveDisplayCurrency).toHaveBeenCalledWith("USD");
    await waitFor(() => expect(usd.getAttribute("aria-pressed")).toBe("true"));
    expect(auto.getAttribute("aria-pressed")).toBe("false");
    expect(changed).toHaveBeenCalledTimes(1);
  });

  it("does not write when the chosen mode is already stored", async () => {
    const port = portWith(async (mode) => ({ ...base, mode }));
    render(<DisplayCurrency port={port} onChanged={() => {}} />);
    await userEvent.click(await screen.findByRole("button", { name: "自动" }));
    expect(port.saveDisplayCurrency).not.toHaveBeenCalled();
  });

  it("keeps the old choice and says why when the kernel refuses", async () => {
    const port = portWith(async () => {
      throw new Error("boom");
    });
    const changed = vi.fn();
    render(<DisplayCurrency port={port} onChanged={changed} />);
    await userEvent.click(await screen.findByRole("button", { name: "人民币 CNY" }));
    await waitFor(() => expect(document.querySelector(".why")).toBeTruthy());
    const pressed = screen.getAllByRole("button").filter((b) => b.getAttribute("aria-pressed") === "true");
    expect(pressed.map((b) => b.textContent)).toEqual(["自动"]);
    expect(changed).not.toHaveBeenCalled();
  });
});

describe("a price the pinned currency cannot be shown for", () => {
  it("names the model, its currency and what to do, and says nothing when there is none", async () => {
    const stuck = { ...base, mode: "CNY" as const, unconverted: [{ provider: "deepseek", model: "deepseek-flash", currency: "USD" }] };
    const port = { displayCurrency: vi.fn(async () => stuck), saveDisplayCurrency: vi.fn() } as unknown as AgentPort;
    render(<DisplayCurrency port={port} onChanged={() => {}} />);
    expect(await screen.findByText(/deepseek-flash（USD）/)).toBeTruthy();
    expect(screen.getByText(/恢复官方价/)).toBeTruthy();
    cleanup();
    const fine = { displayCurrency: vi.fn(async () => ({ ...base, mode: "CNY" as const })), saveDisplayCurrency: vi.fn() } as unknown as AgentPort;
    render(<DisplayCurrency port={fine} onChanged={() => {}} />);
    await screen.findByRole("button", { name: "自动" });
    expect(screen.queryByText(/恢复官方价/)).toBeNull();
  });
});
