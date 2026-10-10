// @vitest-environment jsdom
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { HttpError, type ProviderEntry } from "../port/port";
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

const stored = (name: string): ProviderEntry => ({
  name,
  kind: "openai",
  baseUrl: `https://${name}.example/v1`,
  models: ["chat"],
  default: "chat",
  visionModels: [],
  hasKey: true,
  inUse: name === "relay",
  preset: false,
  canSetVision: true,
});

type Outcome = "ok" | "refused" | "after-write-failure";

function harness(outcome: Outcome) {
  const disk = new Map<string, ProviderEntry>([["relay", stored("relay")], ["other", stored("other")]]);
  const failed = vi.fn();
  const removed: string[] = [];
  const port = {
    providers: vi.fn(async () => [...disk.values()].map((e) => ({ ...e }))),
    protocols: vi.fn(async () => []),
    removeProvider: vi.fn(async (name: string) => {
      removed.push(name);
      if (outcome === "refused") throw new HttpError(409, "running", { code: "provider.running" });
      disk.delete(name);
      if (outcome === "after-write-failure") throw new HttpError(500, "switch model: boom", { error: "switch model: boom" });
    }),
  } as unknown as Port;
  render(<Providers port={port} onChanged={() => {}} onFailed={failed} protocol={{}}
    onProtocol={() => {}} activeKindFor={(a) => a.kinds[0]} />);
  return { port, failed, removed, disk };
}

const detail = () => screen.getByRole("region");
const del = () => within(detail()).getByRole("button", { name: "删除" });

it("drops a removed service from the list", async () => {
  const h = harness("ok");
  await screen.findAllByText("relay.example");
  await userEvent.click(del());
  await waitFor(() => expect(screen.queryAllByText("relay.example")).toHaveLength(0));
  expect(h.removed).toEqual(["relay"]);
  expect(screen.queryByRole("alert")).toBeNull();
});

it("says why a removal was refused next to the button that asked", async () => {
  const h = harness("refused");
  await screen.findAllByText("relay.example");
  await userEvent.click(del());
  const alert = await within(detail()).findByRole("alert");
  expect(alert.textContent).toContain("正在运行");
  expect(h.failed).not.toHaveBeenCalledWith(expect.stringContaining("正在运行"));
  expect(screen.getAllByText("relay.example").length).toBeGreaterThan(0);
});

it("clears the refusal once another service is picked", async () => {
  harness("refused");
  await screen.findAllByText("relay.example");
  await userEvent.click(del());
  await within(detail()).findByRole("alert");
  await userEvent.click(screen.getByText("other.example"));
  await waitFor(() => expect(screen.queryByRole("alert")).toBeNull());
});

it("re-reads the kernel's state after a removal that failed once it was written", async () => {
  const h = harness("after-write-failure");
  await screen.findAllByText("relay.example");
  await userEvent.click(del());
  await waitFor(() => expect(screen.queryAllByText("relay.example")).toHaveLength(0));
  expect(h.disk.has("relay")).toBe(false);
  expect(screen.getAllByRole("alert").length).toBeGreaterThan(0);
});
