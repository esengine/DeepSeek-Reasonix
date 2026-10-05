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

const rows = () => screen.getAllByRole("button").filter((b) => b.dataset.actionClick === "provider.select");
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
  expect(screen.queryByRole("button", { name: /上移 alpha|Move alpha up/ })).toBeNull();
  expect(screen.queryByRole("button", { name: /下移 beta|Move beta down/ })).toBeNull();
  const up = screen.getByRole("button", { name: /上移 beta|Move beta up/ });
  up.focus();
  await userEvent.click(up);
  expect(rows().map((row) => row.querySelector(".nm")?.textContent)).toEqual(["beta", "alpha"]);
  expect(document.activeElement).toBe(rows()[0]);
  expect(screen.queryByRole("button", { name: /上移 beta|Move beta up/ })).toBeNull();
  cleanup();
  draw(list);
  await screen.findAllByText("beta.example");
  expect(rows().map((row) => row.querySelector(".nm")?.textContent)).toEqual(["beta", "alpha"]);
});

it("moves a focused service with Alt+Arrow and keeps focus on its row", async () => {
  draw([entry("alpha", "https://alpha.example"), entry("beta", "https://beta.example"), entry("gamma", "https://gamma.example")]);
  await screen.findAllByText("gamma.example");
  const beta = rows()[1];
  beta.focus();
  await userEvent.keyboard("{Alt>}{ArrowUp}{/Alt}");
  expect(rows().map((row) => row.querySelector(".nm")?.textContent)).toEqual(["beta", "alpha", "gamma"]);
  expect(document.activeElement).toBe(beta);
  await userEvent.keyboard("{Alt>}{ArrowDown}{/Alt}");
  expect(rows().map((row) => row.querySelector(".nm")?.textContent)).toEqual(["alpha", "beta", "gamma"]);
  expect(document.activeElement).toBe(beta);
});

function drawRenamable(list: ProviderEntry[]) {
  let current = list;
  const port = {
    providers: vi.fn(async () => current),
    protocols: vi.fn(async () => []),
    renameProvider: vi.fn(async (names: string[], displayName: string) => {
      current = current.map((p) => (names.includes(p.name) ? { ...p, displayName: displayName || undefined } : p));
    }),
  } as unknown as Port & { renameProvider: ReturnType<typeof vi.fn> };
  const onChanged = vi.fn();
  render(<Providers port={port} onChanged={onChanged} onFailed={() => {}} protocol={{}}
    onProtocol={() => {}} activeKindFor={(a) => a.kinds[0]} />);
  return { port, onChanged };
}

const names = () => rows().map((row) => row.querySelector(".nm")?.textContent);
const list = () => within(document.querySelector(".plist") as HTMLElement);

it("renames an account from its row, relabelling every door and leaving names alone", async () => {
  const doors = [
    { ...entry("relay", "https://relay.example/v1"), keyEnv: "RELAY_KEY" },
    { ...entry("relay-2", "https://relay.example/anthropic"), kind: "anthropic", keyEnv: "RELAY_KEY" },
    entry("beta", "https://beta.example"),
  ];
  const { port, onChanged } = drawRenamable(doors);
  await screen.findAllByText("beta.example");
  await userEvent.click(list().getByRole("button", { name: /重命名 relay（F2）/ }));
  const field = screen.getByRole("textbox", { name: /重命名 relay/ }) as HTMLInputElement;
  expect(field.value).toBe("relay");
  await userEvent.clear(field);
  await userEvent.type(field, "  公司网关 {Enter}");
  expect(port.renameProvider).toHaveBeenCalledWith(["relay", "relay-2"], "公司网关");
  await screen.findByText("公司网关", { selector: ".nm" });
  expect(names()).toEqual(["公司网关", "beta"]);
  expect(within(detail()).getByRole("heading").textContent).toBe("公司网关");
  expect(onChanged).toHaveBeenCalled();
});

it("an emptied name goes back to the derived one, and Escape changes nothing", async () => {
  const { port } = drawRenamable([{ ...entry("alpha", "https://alpha.example"), displayName: "旧名字" }]);
  await screen.findByText("旧名字", { selector: ".nm" });

  const closes = vi.fn();
  window.addEventListener("keydown", closes);
  rows()[0].focus();
  await userEvent.keyboard("{F2}");
  await userEvent.type(screen.getByRole("textbox", { name: /重命名/ }), "别的{Escape}");
  window.removeEventListener("keydown", closes);
  expect(closes).not.toHaveBeenCalledWith(expect.objectContaining({ key: "Escape" }));
  expect(port.renameProvider).not.toHaveBeenCalled();
  expect(names()).toEqual(["旧名字"]);

  await userEvent.click(within(detail()).getByRole("button", { name: /重命名 旧名字/ }));
  const field = screen.getByRole("textbox", { name: /重命名/ }) as HTMLInputElement;
  expect(field.value).toBe("旧名字");
  expect(field.placeholder).toBe("alpha");
  expect(field.maxLength).toBe(64);
  await userEvent.clear(field);
  await userEvent.keyboard("{Enter}");
  expect(port.renameProvider).toHaveBeenCalledWith(["alpha"], "");
  await screen.findByText("alpha", { selector: ".nm" });
});

it("saving the name it already has sends nothing", async () => {
  const { port } = drawRenamable([entry("alpha", "https://alpha.example")]);
  await screen.findAllByText("alpha.example");
  await userEvent.click(list().getByRole("button", { name: /重命名 alpha（F2）/ }));
  await userEvent.keyboard("{Enter}");
  expect(port.renameProvider).not.toHaveBeenCalled();
  expect(screen.queryByRole("textbox", { name: /重命名/ })).toBeNull();
});
