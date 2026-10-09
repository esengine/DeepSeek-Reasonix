// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import "./testkit";
import { t } from "../i18n";
import type { AgentPort, ModelEntry, ProviderEntry, UsageReport } from "../port/port";
import { Usage } from "./Usage";

const REPORT: UsageReport = {
  from: "2026-08-01",
  to: "2026-08-31",
  tokens: 2_468,
  requests: 12,
  turns: 9,
  cache_hit: 2_000,
  cache_miss: 468,
  cost: [{ amount: "1.23", currency: "CNY" }],
  active_days: 2,
  top_model: "relay/model-a",
  top_provider: "relay-a",
  daily: [
    {
      day: "2026-08-15", total: 1_234, byModel: { "relay/model-a": 1_234 },
      byProvider: { "relay-a": 1_234 }, requests: 6, turns: 4,
      cacheHit: 1_000, cacheMiss: 234, cost: [{ amount: "0.50", currency: "CNY" }],
    },
    {
      day: "2026-08-16", total: 1_234, byModel: { "relay/model-b": 1_234 },
      byProvider: { "relay-b": 1_234 }, requests: 6, turns: 5,
      cacheHit: 1_000, cacheMiss: 234, cost: [{ amount: "0.73", currency: "CNY" }],
    },
  ],
  models: [
    { model: "relay/model-a", provider: "relay-a", tokens: 1_234, percent: 50 },
    { model: "relay/model-b", provider: "relay-b", tokens: 1_234, percent: 50 },
  ],
  providers: [
    { provider: "relay-a", tokens: 1_234, percent: 50 },
    { provider: "relay-b", tokens: 1_234, percent: 50 },
  ],
};

function provider(name: string, displayName?: string): ProviderEntry {
  return {
    name, displayName, kind: "openai", baseUrl: "https://relay.example", models: ["model"],
    default: "model", hasKey: true, inUse: true, preset: false,
  };
}

function model(providerName: string, kind: string, vendor: string): ModelEntry {
  return { ref: `${providerName}/model`, provider: providerName, model: "model", kind, vendor };
}

function makePort(over: Partial<AgentPort> = {}) {
  return {
    usage: vi.fn().mockResolvedValue(REPORT),
    providers: vi.fn().mockResolvedValue([]),
    models: vi.fn().mockResolvedValue([]),
    ...over,
  } as unknown as AgentPort;
}

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

describe("usage is exact on every device", () => {
  it("lists each day's full count instead of leaving it to a hover", async () => {
    render(<Usage port={makePort()} />);

    const table = await screen.findByRole("table", { name: t("每日明细") });
    const row = table.querySelector<HTMLElement>('[data-day="2026-08-15"]');
    expect(row).toBeTruthy();
    expect(within(row!).getByText("2026-08-15")).toBeTruthy();
    expect(within(row!).getByText("1,234")).toBeTruthy();
    expect(within(row!).getByText("6")).toBeTruthy();
    expect(screen.getByText(t("左右滚动查看"))).toBeTruthy();
  });

  it("shows the configured provider label and a distinct bar colour", async () => {
    const port = makePort({
      providers: vi.fn().mockResolvedValue([
        provider("relay-a", "Relay Alpha"),
        provider("relay-b", "Relay Beta"),
      ]),
    });
    render(<Usage port={port} />);

    expect(await screen.findByText("Relay Alpha")).toBeTruthy();
    expect(screen.getByText("Relay Beta")).toBeTruthy();
    expect(screen.queryByText("relay-a")).toBeNull();
    const fillA = screen.getByText("Relay Alpha").closest(".urow")?.querySelector<HTMLElement>(".ufill");
    const fillB = screen.getByText("Relay Beta").closest(".urow")?.querySelector<HTMLElement>(".ufill");
    const colorA = fillA?.style.getPropertyValue("--row-color") ?? "";
    const colorB = fillB?.style.getPropertyValue("--row-color") ?? "";
    expect(colorA).toMatch(/^oklch\(from var\(--chart\) l c \d+\)$/);
    expect(colorB).toMatch(/^oklch\(from var\(--chart\) l c \d+\)$/);
    expect(colorA).not.toBe(colorB);
  });

  it("uses provider kind and host when no display name is configured", async () => {
    const port = makePort({
      usage: vi.fn().mockResolvedValue({ ...REPORT, providers: [REPORT.providers[0]], models: [REPORT.models[0]] }),
      providers: vi.fn().mockResolvedValue([provider("relay-a")]),
    });
    render(<Usage port={port} />);

    expect(await screen.findByText("openai · relay.example")).toBeTruthy();
    expect(screen.queryByText("relay-a")).toBeNull();
  });

  it("uses model kind and host when the provider is missing from providers()", async () => {
    const port = makePort({
      usage: vi.fn().mockResolvedValue({ ...REPORT, providers: [REPORT.providers[0]], models: [REPORT.models[0]] }),
      models: vi.fn().mockResolvedValue([model("relay-a", "anthropic", "gateway.example")]),
    });
    render(<Usage port={port} />);

    expect(await screen.findByText("anthropic · gateway.example")).toBeTruthy();
    expect(screen.queryByText("relay-a")).toBeNull();
  });
});

describe("usage can be asked for another window", () => {
  it("accepts an explicit inclusive date range", async () => {
    const port = makePort();
    render(<Usage port={port} />);
    await screen.findByText("2026-08-01 → 2026-08-31");

    fireEvent.click(screen.getByRole("button", { name: t("自定义") }));
    fireEvent.change(screen.getByLabelText(t("开始日期")), { target: { value: "2026-08-05" } });
    fireEvent.change(screen.getByLabelText(t("结束日期")), { target: { value: "2026-08-20" } });
    fireEvent.click(screen.getByRole("button", { name: t("应用") }));

    await waitFor(() => expect(port.usage).toHaveBeenLastCalledWith({ from: "2026-08-05", to: "2026-08-20" }));
  });

  it("turns a calendar month into its first and last day", async () => {
    const port = makePort();
    render(<Usage port={port} />);
    await screen.findByText("2026-08-01 → 2026-08-31");

    fireEvent.click(screen.getByRole("button", { name: t("自然月") }));
    fireEvent.change(screen.getByLabelText(t("选择月份")), { target: { value: "2026-08" } });

    await waitFor(() => expect(port.usage).toHaveBeenLastCalledWith({ from: "2026-08-01", to: "2026-08-31" }));
  });

  it("keeps the range controls visible when the server refuses a window", async () => {
    const port = makePort({
      usage: vi.fn((query) => "days" in query
        ? Promise.resolve(REPORT)
        : Promise.reject(new Error("range must be between 1 and 365 days"))),
    });
    render(<Usage port={port} />);
    await screen.findByText("2026-08-01 → 2026-08-31");

    fireEvent.click(screen.getByRole("button", { name: t("自定义") }));
    fireEvent.change(screen.getByLabelText(t("开始日期")), { target: { value: "2024-01-01" } });
    fireEvent.change(screen.getByLabelText(t("结束日期")), { target: { value: "2025-12-31" } });
    fireEvent.click(screen.getByRole("button", { name: t("应用") }));

    expect((await screen.findByRole("alert")).textContent).toContain("range must be between 1 and 365 days");
    expect(screen.getByRole("button", { name: t("自定义") })).toBeTruthy();
    expect(screen.getByLabelText(t("开始日期"))).toBeTruthy();
  });
});
