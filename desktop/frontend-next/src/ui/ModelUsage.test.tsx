// @vitest-environment jsdom
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import type { ModelEntry, RoleAssignments } from "../port/port";
import { ModelUsage } from "./ModelUsage";
import { writeProviderOrder } from "../state/providerorder";
import { accountKey } from "./vendors";

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

const MODELS: ModelEntry[] = [
  { ref: "deepseek/deepseek-flash", provider: "deepseek", vendor: "api.deepseek.com", model: "deepseek-flash", kind: "anthropic", vision: true, contextWindow: 1_000_000 },
  { ref: "deepseek/deepseek-pro", provider: "deepseek", vendor: "api.deepseek.com", model: "deepseek-pro", kind: "anthropic" },
];
const ROLES: RoleAssignments = { planner: "", subagent: "deepseek/deepseek-pro", vision: "", guardian: "", decision: "" };

function draw() {
  const onMain = vi.fn();
  const onRole = vi.fn();
  render(<ModelUsage models={MODELS} roles={ROLES} main="deepseek/deepseek-flash" busy="" protocol={{}} onMain={onMain} onRole={onRole} />);
  return { onMain, onRole };
}

const row = (name: string) => screen.getByText(name).closest("[role=row]") as HTMLElement;

it("names each use, the model doing it, and the service it goes through", () => {
  draw();
  expect((screen.getByRole("combobox", { name: "默认模型" }) as HTMLSelectElement).value).toBe("deepseek/deepseek-flash");
  expect(row("默认模型").textContent).toContain("读图 · 上下文 1M");
  expect(row("计划").textContent).toContain("随主模型");
  expect((screen.getByRole("combobox", { name: "子代理" }) as HTMLSelectElement).value).toBe("deepseek/deepseek-pro");
});

it("says decision has no source instead of offering the chat models", () => {
  draw();
  const decision = screen.getByRole("combobox", { name: "决策" }) as HTMLSelectElement;
  expect(decision.disabled).toBe(true);
  expect(decision.textContent).toBe("尚无可用来源");
});

it("writes the row that changed", async () => {
  const { onMain, onRole } = draw();
  await userEvent.selectOptions(screen.getByRole("combobox", { name: "看图" }), "deepseek/deepseek-flash");
  expect(onRole).toHaveBeenCalledWith("vision", "deepseek/deepseek-flash");
  await userEvent.selectOptions(screen.getByRole("combobox", { name: "默认模型" }), "deepseek/deepseek-pro");
  expect(onMain).toHaveBeenCalledWith("deepseek/deepseek-pro");
});

it("lists services in the saved order without reordering models within one service", () => {
  const other: ModelEntry = { ref: "other/chat", provider: "other", vendor: "other.example", model: "chat" };
  writeProviderOrder([accountKey("other.example"), accountKey("api.deepseek.com")]);
  render(<ModelUsage models={[...MODELS, other]} roles={ROLES} main={MODELS[0].ref} busy="" protocol={{}} onMain={() => {}} onRole={() => {}} />);
  const groups = screen.getByRole("combobox", { name: "默认模型" }).querySelectorAll("optgroup");
  expect([...groups].map((group) => group.label)).toEqual(["other", "deepseek"]);
  expect([...groups[1].querySelectorAll("option")].map((option) => option.value))
    .toEqual(["deepseek/deepseek-flash", "deepseek/deepseek-pro"]);
});
