// @vitest-environment jsdom
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { cleanup, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import type { ProviderEntry } from "../port/port";
import { Providers, type Port } from "./Providers";

beforeEach(() => {
  const values = new Map<string, string>();
  vi.stubGlobal("localStorage", {
    getItem: (key: string) => values.get(key) ?? null,
    setItem: (key: string, value: string) => void values.set(key, value),
  });
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const entry = (name: string, baseUrl: string, inUse = false): ProviderEntry => ({
  name,
  kind: "openai",
  baseUrl,
  models: [`${name}-chat`],
  default: `${name}-chat`,
  hasKey: true,
  inUse,
  preset: false,
});

function draw(list: ProviderEntry[]) {
  const port = {
    providers: vi.fn(async () => list),
    protocols: vi.fn(async () => []),
  } as unknown as Port;
  render(<Providers port={port} onChanged={() => {}} onFailed={() => {}} protocol={{}}
    onProtocol={() => {}} activeKindFor={(a) => a.kinds[0]} />);
}

const rows = () => screen.getAllByRole("button").filter((b) => b.dataset.action === "provider.select");
const detail = () => screen.getByRole("region");

it("opens on the service in use and shows its fields without a second click", async () => {
  draw([entry("alpha", "https://alpha.example"), entry("beta", "https://beta.example", true)]);
  await screen.findAllByText("beta.example");
  expect(rows().map((r) => r.getAttribute("aria-pressed"))).toEqual(["false", "true"]);
  expect(within(detail()).getByDisplayValue("https://beta.example")).toBeTruthy();
});

it("picking a service in the list shows that service", async () => {
  draw([entry("alpha", "https://alpha.example", true), entry("beta", "https://beta.example")]);
  await screen.findAllByText("beta.example");
  await userEvent.click(rows()[1]);
  expect(rows()[1].getAttribute("aria-pressed")).toBe("true");
  expect(within(detail()).getByDisplayValue("https://beta.example")).toBeTruthy();
});

it("adding a service takes the detail side and leaves the list in place", async () => {
  draw([entry("alpha", "https://alpha.example", true)]);
  await screen.findAllByText("alpha.example");
  await userEvent.click(screen.getByRole("button", { name: /添加模型服务/ }));
  expect(screen.queryByRole("region")).toBeNull();
  expect(screen.getByText("添加模型来源")).toBeTruthy();
  expect(rows()).toHaveLength(1);
  expect(rows()[0].getAttribute("aria-pressed")).toBe("false");
});

it("reorders connections without editing their credentials and restores the choice", async () => {
  const list = [entry("alpha", "https://alpha.example"), entry("beta", "https://beta.example")];
  draw(list);
  await screen.findAllByText("beta.example");
  await userEvent.click(screen.getByRole("button", { name: /上移 beta|Move beta up/ }));
  expect(rows().map((row) => row.querySelector(".nm")?.textContent)).toEqual(["beta", "alpha"]);
  cleanup();
  draw(list);
  await screen.findAllByText("beta.example");
  expect(rows().map((row) => row.querySelector(".nm")?.textContent)).toEqual(["beta", "alpha"]);
});
