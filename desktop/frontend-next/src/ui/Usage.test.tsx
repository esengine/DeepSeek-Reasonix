// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import "./testkit";
import { t } from "../i18n";
import type { AgentPort, ModelEntry, ProviderEntry, UsageHour, UsageReport } from "../port/port";
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

describe("the charts, as the Usage-Page board lays them out", () => {
  it("keeps every currency's spend chart in one column beside the usage chart", async () => {
    const two: UsageReport = {
      ...REPORT,
      cost: [{ amount: "1.23", currency: "CNY" }, { amount: "0.01", currency: "USD" }],
      daily: REPORT.daily.map((d, i) => ({ ...d, cost: [{ amount: "0.50", currency: i === 0 ? "CNY" : "USD" }] })),
    };
    render(<Usage port={makePort({ usage: vi.fn().mockResolvedValue(two) })} />);
    await screen.findByRole("table", { name: t("usage.daily_details") });
    const grid = document.querySelector(".ucharts")!;
    expect([...grid.children].map((c) => c.className)).toEqual(["ucard", "ucharts-spend"]);
    expect(grid.querySelectorAll(".ucharts-spend > .ucard")).toHaveLength(2);
  });
});

describe("usage is exact on every device", () => {
  it("lists each day's full count instead of leaving it to a hover", async () => {
    render(<Usage port={makePort()} />);

    const table = await screen.findByRole("table", { name: t("usage.daily_details") });
    const row = table.querySelector<HTMLElement>('[data-day="2026-08-15"]');
    expect(row).toBeTruthy();
    expect(within(row!).getByText("2026-08-15")).toBeTruthy();
    expect(within(row!).getByText("1,234")).toBeTruthy();
    expect(within(row!).getByText("6")).toBeTruthy();
    expect(screen.getByText(t("usage.scroll_sideways_for_all_columns"))).toBeTruthy();
  });

  it("lists the newest day first, so the latest figures are in view without scrolling", async () => {
    render(<Usage port={makePort()} />);

    const table = await screen.findByRole("table", { name: t("usage.daily_details") });
    const days = [...table.querySelectorAll<HTMLElement>("tbody tr")].map((row) => row.dataset.day);
    expect(days).toEqual(["2026-08-16", "2026-08-15"]);
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

const TODAY: UsageReport = {
  ...REPORT,
  from: "2026-08-16",
  to: "2026-08-16",
  tokens: 1_234,
  requests: 6,
  turns: 5,
  cache_hit: 1_000,
  cache_miss: 234,
  cost: [{ amount: "0.73", currency: "CNY" }],
  active_days: 1,
  daily: [REPORT.daily[1]],
  hourly: Array.from({ length: 24 }, (_, hour): UsageHour => ({
    hour: `${String(hour).padStart(2, "0")}:00`,
    total: hour === 9 ? 1_234 : 0,
    byModel: hour === 9 ? { "relay/model-b": 1_234 } : {},
    byProvider: hour === 9 ? { "relay-b": 1_234 } : {},
    requests: hour === 9 ? 6 : 0,
    turns: hour === 9 ? 5 : 0,
    cacheHit: hour === 9 ? 1_000 : 0,
    cacheMiss: hour === 9 ? 234 : 0,
    cost: hour === 9 ? [{ amount: "0.73", currency: "CNY" }] : undefined,
  })),
  models: [REPORT.models[1]],
  providers: [REPORT.providers[1]],
};

function localToday(): string {
  const now = new Date();
  return `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, "0")}-${String(now.getDate()).padStart(2, "0")}`;
}

function oneDayPort() {
  return makePort({
    usage: vi.fn((query) => Promise.resolve("days" in query ? REPORT : TODAY)),
  });
}

describe("a one-day window reads in hours", () => {
  it("asks for today as one explicit calendar day", async () => {
    const port = oneDayPort();
    render(<Usage port={port} />);
    await screen.findByText("2026-08-01 → 2026-08-31");

    fireEvent.click(screen.getByRole("button", { name: t("usage.today") }));

    const today = localToday();
    await waitFor(() => expect(port.usage).toHaveBeenLastCalledWith({ from: today, to: today }));
  });

  it("draws the day's hours instead of a single point", async () => {
    const port = oneDayPort();
    render(<Usage port={port} />);
    fireEvent.click(await screen.findByRole("button", { name: t("usage.today") }));

    expect(await screen.findByRole("img", { name: t("usage.hourly_tokens") })).toBeTruthy();
    const table = await screen.findByRole("table", { name: t("usage.hourly_details") });
    const row = table.querySelector<HTMLElement>('[data-hour="09:00"]');
    expect(row).toBeTruthy();
    expect(within(row!).getByText("1,234")).toBeTruthy();
    expect(within(row!).getByText("6")).toBeTruthy();
  });

  it("keeps an empty day's y axis readable", async () => {
    const empty: UsageReport = {
      ...TODAY,
      tokens: 0,
      requests: 0,
      turns: 0,
      cache_hit: 0,
      cache_miss: 0,
      cost: [],
      daily: [{ ...TODAY.daily[0], total: 0, requests: 0, turns: 0, cacheHit: 0, cacheMiss: 0, cost: undefined }],
      hourly: TODAY.hourly!.map((hour) => ({
        ...hour, total: 0, requests: 0, turns: 0, cacheHit: 0, cacheMiss: 0, cost: undefined,
      })),
    };
    render(<Usage port={makePort({ usage: vi.fn().mockResolvedValue(empty) })} />);

    const chart = await screen.findByRole("img", { name: t("usage.hourly_tokens") });
    const ticks = [...chart.querySelectorAll("text")]
      .map((node) => node.textContent)
      .filter((value) => value === "0" || value === "1" || value === "2");
    expect(new Set(ticks).size).toBe(ticks.length);
  });

  it("uses the hour series for a custom one-day range", async () => {
    const port = oneDayPort();
    render(<Usage port={port} />);
    await screen.findByText("2026-08-01 → 2026-08-31");

    fireEvent.click(screen.getByRole("button", { name: t("common.custom") }));
    fireEvent.change(screen.getByLabelText(t("usage.start_date")), { target: { value: "2026-08-16" } });
    fireEvent.change(screen.getByLabelText(t("usage.end_date")), { target: { value: "2026-08-16" } });
    fireEvent.click(screen.getByRole("button", { name: t("usage.apply") }));

    expect(await screen.findByRole("table", { name: t("usage.hourly_details") })).toBeTruthy();
  });
});

describe("usage can be asked for another window", () => {
  it("accepts an explicit inclusive date range", async () => {
    const port = makePort();
    render(<Usage port={port} />);
    await screen.findByText("2026-08-01 → 2026-08-31");

    fireEvent.click(screen.getByRole("button", { name: t("common.custom") }));
    fireEvent.change(screen.getByLabelText(t("usage.start_date")), { target: { value: "2026-08-05" } });
    fireEvent.change(screen.getByLabelText(t("usage.end_date")), { target: { value: "2026-08-20" } });
    fireEvent.click(screen.getByRole("button", { name: t("usage.apply") }));

    await waitFor(() => expect(port.usage).toHaveBeenLastCalledWith({ from: "2026-08-05", to: "2026-08-20" }));
  });

  it("turns a calendar month into its first and last day", async () => {
    const port = makePort();
    render(<Usage port={port} />);
    await screen.findByText("2026-08-01 → 2026-08-31");

    fireEvent.click(screen.getByRole("button", { name: t("usage.calendar_month") }));
    fireEvent.change(screen.getByLabelText(t("usage.choose_month")), { target: { value: "2026-08" } });

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

    fireEvent.click(screen.getByRole("button", { name: t("common.custom") }));
    fireEvent.change(screen.getByLabelText(t("usage.start_date")), { target: { value: "2024-01-01" } });
    fireEvent.change(screen.getByLabelText(t("usage.end_date")), { target: { value: "2025-12-31" } });
    fireEvent.click(screen.getByRole("button", { name: t("usage.apply") }));

    expect((await screen.findByRole("alert")).textContent).toContain("range must be between 1 and 365 days");
    expect(screen.getByRole("button", { name: t("common.custom") })).toBeTruthy();
    expect(screen.getByLabelText(t("usage.start_date"))).toBeTruthy();
  });
});
