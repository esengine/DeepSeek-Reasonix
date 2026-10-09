// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import type { ProviderEntry } from "../port/port";
import type { Port } from "./Providers";
import { EditConn } from "./EditConn";

afterEach(cleanup);

const relay = (over: Partial<ProviderEntry> = {}): ProviderEntry => ({
  name: "relay", kind: "openai", baseUrl: "https://relay.invalid/v1",
  models: ["gpt-5.6-sol", "qwen3.8-max", "glm-5"], default: "gpt-5.6-sol",
  hasKey: true, inUse: false, preset: false,
  supportedEfforts: ["low", "medium", "high"],
  inheritedEfforts: {
    "gpt-5.6-sol": { supportedEfforts: ["low", "medium", "high"] },
    "qwen3.8-max": { supportedEfforts: ["low", "medium", "high"] },
    "glm-5": { supportedEfforts: ["low", "medium", "high"] },
  },
  ...over,
});

const row = (model: string) => document.querySelector(`.me-row[data-model="${model}"]`) as HTMLElement;

it("saves a model's own levels and leaves the others inheriting", async () => {
  const editProvider = vi.fn(async () => {});
  render(<EditConn entry={relay()} port={{ editProvider } as unknown as Port} busy="" setBusy={() => {}} onDone={() => {}} onRevert={() => {}} declare />);

  expect(within(row("glm-5")).getByText("继承：low · medium · high")).toBeTruthy();
  await userEvent.selectOptions(screen.getByRole("combobox", { name: "qwen3.8-max 的推理档位" }), "own");
  const qwen = row("qwen3.8-max");
  await userEvent.click(within(qwen).getByRole("button", { name: "high" }));
  await userEvent.click(within(qwen).getByRole("button", { name: "none" }));
  await userEvent.click(within(qwen).getByRole("button", { name: "xhigh" }));
  await userEvent.selectOptions(within(qwen).getByRole("combobox", { name: "qwen3.8-max 的默认档位" }), "xhigh");
  await userEvent.click(screen.getByRole("button", { name: "保存" }));

  await waitFor(() => expect(editProvider).toHaveBeenCalled());
  expect(editProvider).toHaveBeenCalledWith(expect.objectContaining({
    supportedEfforts: ["low", "medium", "high"],
    modelEfforts: { "qwen3.8-max": { supportedEfforts: ["none", "low", "medium", "xhigh"], defaultEffort: "xhigh" } },
  }));
});

it("turns a stored declaration back into inheritance", async () => {
  const editProvider = vi.fn(async () => {});
  const entry = relay({ modelEfforts: { "glm-5": { supportedEfforts: ["enabled", "disabled"] } } });
  render(<EditConn entry={entry} port={{ editProvider } as unknown as Port} busy="" setBusy={() => {}} onDone={() => {}} onRevert={() => {}} declare />);

  expect(within(row("glm-5")).getByRole("button", { name: "enabled" }).getAttribute("aria-pressed")).toBe("true");
  await userEvent.selectOptions(screen.getByRole("combobox", { name: "glm-5 的推理档位" }), "inherit");
  await userEvent.click(screen.getByRole("button", { name: "保存" }));

  await waitFor(() => expect(editProvider).toHaveBeenCalled());
  expect(editProvider).toHaveBeenCalledWith(expect.objectContaining({ modelEfforts: {} }));
});

it("adds a level no chip names", async () => {
  const editProvider = vi.fn(async () => {});
  render(<EditConn entry={relay()} port={{ editProvider } as unknown as Port} busy="" setBusy={() => {}} onDone={() => {}} onRevert={() => {}} declare />);

  await userEvent.selectOptions(screen.getByRole("combobox", { name: "glm-5 的推理档位" }), "own");
  await userEvent.type(screen.getByRole("textbox", { name: "glm-5 的其他档位" }), "Ultra{enter}");
  expect(within(row("glm-5")).getByRole("button", { name: "ultra" }).getAttribute("aria-pressed")).toBe("true");
});

it("holds a model on a fixed-vocabulary protocol out of per-model levels", () => {
  const entry = relay({ modelProtocols: { "qwen3.8-max": "kimi-k3" } });
  render(<EditConn entry={entry} port={{ editProvider: vi.fn() } as unknown as Port} busy="" setBusy={() => {}} onDone={() => {}} onRevert={() => {}} declare />);
  const mode = screen.getByRole("combobox", { name: "qwen3.8-max 的推理档位" }) as HTMLSelectElement;
  expect(mode.disabled).toBe(true);
  expect(within(row("qwen3.8-max")).getByText("该模型的思考参数不使用自定义档位。")).toBeTruthy();
});

it("says inheritance waits on the save once the connection's levels are cleared", async () => {
  render(<EditConn entry={relay()} port={{ editProvider: vi.fn() } as unknown as Port} busy="" setBusy={() => {}} onDone={() => {}} onRevert={() => {}} declare />);
  await userEvent.clear(screen.getByRole("textbox", { name: /^推理档位/ }));
  expect(within(row("glm-5")).getByText("继承：保存后按接入设置确定")).toBeTruthy();
});
