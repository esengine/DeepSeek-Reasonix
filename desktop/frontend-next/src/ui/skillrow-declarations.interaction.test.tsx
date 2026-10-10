// @vitest-environment jsdom
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { cleanup, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { boot, STORAGE } from "../i18n";
import type { AgentPort, SkillEntry } from "../port/port";
import { SkillRow } from "./SkillRow";

beforeEach(() => {
  localStorage.setItem(STORAGE, "zh");
  boot();
});
afterEach(cleanup);

const skill: SkillEntry = {
  name: "review", slashName: "review", enabled: true, subagent: true,
  model: "author/provider:model", effort: "high", allowedTools: ["read_file", "mcp__docs__lookup"],
};

function draw(over: Partial<SkillEntry> = {}) {
  const port = { setSkillEnabled: vi.fn().mockResolvedValue(undefined), clearSkillOverride: vi.fn().mockResolvedValue(undefined) } as unknown as AgentPort;
  const onDone = vi.fn();
  const onFailed = vi.fn();
  const row = (sk: SkillEntry, root = "/workspace/main", active = port) => <SkillRow
    sk={sk} implicit port={active} root={root} onDone={onDone} onFailed={onFailed}
  />;
  const view = render(row({ ...skill, ...over }));
  return { ...view, port, onDone, onFailed, row };
}

function declaration() {
  const summary = screen.getByText("子代理声明");
  const details = summary.closest("details")!;
  return { summary, details, body: within(details) };
}

it("lets the reader inspect literal profile declarations without running or changing the skill", async () => {
  const { port, onDone, onFailed } = draw();
  const { summary, details, body } = declaration();
  expect(summary.getAttribute("aria-label")).toBe("review 的子代理声明");
  expect(details.open).toBe(false);
  await userEvent.tab();
  expect(document.activeElement).toBe(screen.getByRole("switch"));
  await userEvent.tab();
  expect(document.activeElement).toBe(summary);
  await userEvent.click(summary);
  expect(details.open).toBe(true);
  expect(body.getByText("声明的模型")).toBeTruthy();
  expect(body.getByText("author/provider:model")).toBeTruthy();
  expect(body.getByText("声明的推理强度")).toBeTruthy();
  expect(body.getByText("high")).toBeTruthy();
  expect(body.getByText("声明的工具")).toBeTruthy();
  expect(body.getByText("read_file")).toBeTruthy();
  expect(body.getByText("mcp__docs__lookup")).toBeTruthy();
  expect(body.getByText("显示技能自身声明；实际运行配置还会结合当前设置解析。")).toBeTruthy();
  await userEvent.click(summary);
  expect(details.open).toBe(false);
  expect(port.setSkillEnabled).not.toHaveBeenCalled();
  expect(port.clearSkillOverride).not.toHaveBeenCalled();
  expect(onDone).not.toHaveBeenCalled();
  expect(onFailed).not.toHaveBeenCalled();
});

it.each<[string, Partial<SkillEntry>, string, string]>([
  ["model", { model: "custom", effort: undefined, allowedTools: undefined }, "声明的模型", "custom"],
  ["effort", { model: undefined, effort: "medium", allowedTools: [] }, "声明的推理强度", "medium"],
  ["tools", { model: undefined, effort: undefined, allowedTools: ["read_file"] }, "声明的工具", "read_file"],
])("shows only the supplied %s declaration", async (_, over, label, value) => {
  draw(over);
  const { summary, body } = declaration();
  await userEvent.click(summary);
  expect(body.getAllByRole("term").map((el) => el.textContent)).toEqual([label]);
  expect(body.getByText(value)).toBeTruthy();
  expect(body.queryByText("默认")).toBeNull();
});

it.each([
  { subagent: false },
  { model: undefined, effort: undefined, allowedTools: undefined },
  { model: "", effort: "", allowedTools: [] },
])("does not invent a subagent profile when it is absent (%j)", (over) => {
  draw(over);
  expect(screen.queryByText("子代理声明")).toBeNull();
  expect(screen.queryByText("author/provider:model")).toBeNull();
  expect(screen.getByRole("switch").getAttribute("aria-checked")).toBe("true");
});

it("keeps disabled declarations inspectable and the project toggle independent", async () => {
  const { port, onDone } = draw({ enabled: false });
  const { summary, details } = declaration();
  await userEvent.click(summary);
  expect(details.open).toBe(true);
  await userEvent.click(screen.getByRole("switch", { name: "启用 review" }));
  expect(port.setSkillEnabled).toHaveBeenCalledExactlyOnceWith("review", true, "project", "/workspace/main");
  expect(onDone).toHaveBeenCalledTimes(1);
  expect(details.open).toBe(true);
  expect(screen.getByRole("switch").getAttribute("aria-checked")).toBe("false");
});

it("keeps clearing a project override independent of disclosure", async () => {
  const { port, onDone } = draw({ switchScope: "project" });
  const { details } = declaration();
  await userEvent.click(screen.getByRole("button", { name: "仅本项目" }));
  expect(port.clearSkillOverride).toHaveBeenCalledExactlyOnceWith("review", "/workspace/main");
  expect(onDone).toHaveBeenCalledTimes(1);
  expect(details.open).toBe(false);
});

it("reads refreshed declarations from the selected project and connection", async () => {
  const { row, rerender, port } = draw();
  await userEvent.click(declaration().summary);
  const longTool = "mcp__" + "a".repeat(180) + "__lookup";
  rerender(row({ ...skill, model: "next/model", effort: undefined, allowedTools: [longTool] }, "/workspace/other", { ...port }));
  const { body } = declaration();
  expect(body.queryByText("author/provider:model")).toBeNull();
  expect(body.queryByText("high")).toBeNull();
  expect(body.queryByText("read_file")).toBeNull();
  expect(body.getByText("next/model")).toBeTruthy();
  expect(body.getByText(longTool).textContent).toBe(longTool);
});

it("labels declarations in English while keeping author values literal", async () => {
  localStorage.setItem(STORAGE, "en");
  boot();
  draw();
  const summary = screen.getByText("Subagent declarations");
  expect(summary.getAttribute("aria-label")).toBe("Subagent declarations for review");
  await userEvent.click(summary);
  const body = within(summary.closest("details")!);
  expect(body.getByText("Declared model")).toBeTruthy();
  expect(body.getByText("Declared reasoning effort")).toBeTruthy();
  expect(body.getByText("Declared tools")).toBeTruthy();
  expect(body.getByText("Shows the skill's declarations. Runtime configuration also depends on current settings.")).toBeTruthy();
  expect(body.getByText("author/provider:model")).toBeTruthy();
  expect(body.getByText("high")).toBeTruthy();
});
