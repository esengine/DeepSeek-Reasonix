// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import type { ProviderEntry } from "../port/port";
import type { Port } from "./Providers";
import { EditConn } from "./EditConn";

afterEach(cleanup);

const relay = (over: Partial<ProviderEntry> = {}): ProviderEntry => ({
  name: "relay", kind: "openai", baseUrl: "https://relay.invalid/v1",
  models: ["big", "small"], default: "big", hasKey: true, inUse: false, preset: false,
  contextWindow: 131072,
  inheritedLimits: { big: { contextWindow: 131072 }, small: { contextWindow: 131072 } },
  ...over,
});

const mount = (entry: ProviderEntry, editProvider = vi.fn(async () => {})) => {
  render(<EditConn entry={entry} port={{ editProvider } as unknown as Port} busy="" setBusy={() => {}} onDone={() => {}} onRevert={() => {}} />);
  return editProvider;
};

it("shows the inherited value as a placeholder and never sends it as an override", async () => {
  const editProvider = mount(relay());
  const win = screen.getByRole("textbox", { name: "small 的上下文窗口" }) as HTMLInputElement;
  expect(win.value).toBe("");
  expect(win.placeholder).toBe("继承 131,072");
  await userEvent.type(screen.getByRole("textbox", { name: "上下文窗口" }), "1");
  await userEvent.click(screen.getByRole("button", { name: "保存" }));
  await waitFor(() => expect(editProvider).toHaveBeenCalled());
  expect(editProvider).toHaveBeenCalledWith(expect.objectContaining({ modelLimits: {} }));
});

it("sends one model's own window and output cap, digits only", async () => {
  const editProvider = mount(relay());
  await userEvent.type(screen.getByRole("textbox", { name: "small 的上下文窗口" }), "32,000");
  await userEvent.type(screen.getByRole("textbox", { name: "small 的最大输出" }), "4096");
  await userEvent.click(screen.getByRole("button", { name: "保存" }));
  await waitFor(() => expect(editProvider).toHaveBeenCalled());
  expect(editProvider).toHaveBeenCalledWith(expect.objectContaining({
    contextWindow: 131072, modelLimits: { small: { contextWindow: 32000, maxOutputTokens: 4096 } },
  }));
});

it("clearing a stored override sends the model without limits", async () => {
  const editProvider = mount(relay({ modelLimits: { small: { contextWindow: 32000 } } }));
  const win = screen.getByRole("textbox", { name: "small 的上下文窗口" }) as HTMLInputElement;
  expect(win.value).toBe("32000");
  await userEvent.clear(win);
  await userEvent.click(screen.getByRole("button", { name: "保存" }));
  await waitFor(() => expect(editProvider).toHaveBeenCalled());
  expect(editProvider).toHaveBeenCalledWith(expect.objectContaining({ modelLimits: {} }));
});

it("follows a provider-wide window typed but not yet saved", async () => {
  mount(relay());
  const wide = screen.getByRole("textbox", { name: "上下文窗口" });
  await userEvent.clear(wide);
  await userEvent.type(wide, "200000");
  expect((screen.getByRole("textbox", { name: "big 的上下文窗口" }) as HTMLInputElement).placeholder).toBe("继承 200,000");
});

it("keeps a hand-written no-cap output limit while its field is untouched", async () => {
  const editProvider = mount(relay({ modelLimits: { small: { contextWindow: 8000, maxOutputTokens: -1 } } }));
  await userEvent.type(screen.getByRole("textbox", { name: "上下文窗口" }), "1");
  await userEvent.click(screen.getByRole("button", { name: "保存" }));
  await waitFor(() => expect(editProvider).toHaveBeenCalled());
  expect(editProvider).toHaveBeenCalledWith(expect.objectContaining({
    modelLimits: { small: { contextWindow: 8000, maxOutputTokens: -1 } },
  }));
});
