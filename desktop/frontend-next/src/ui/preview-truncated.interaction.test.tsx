// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { AddPlugin, Candidate, PreviewCut } from "./AddPlugin";
import { MockPort } from "../port/mock";
import { PlanConfirm } from "./MarketConfirm";
import type { AgentPort, MarketPlan, PluginAction, PluginPlan } from "../port/port";

afterEach(cleanup);

const action: PluginAction = { kind: "plugin", name: "notes", action: "install_plugin_package", status: "planned", riskLevel: "high" };
const plan = (over: Partial<MarketPlan>): MarketPlan => ({
  ok: true, applied: false, status: "planned", slug: "demo/notes", version: "1.0.0", planId: "p1", actions: [action], ...over,
});
const draw = (p: MarketPlan) =>
  render(<PlanConfirm slug="demo/notes" plan={p} busy={false} error="" onCancel={() => {}} onInstall={() => {}} />);

it("market confirmation says so when only the plan-level text was cut", () => {
  draw(plan({ previewTruncated: true, warnings: ["w"] }));
  expect(screen.getByTestId("preview-cut")).toBeTruthy();
});

it("market confirmation stays quiet for a complete plan", () => {
  draw(plan({ warnings: ["w"] }));
  expect(screen.queryByTestId("preview-cut")).toBeNull();
});

it("a cut step is marked on its own row", () => {
  const { container } = render(<Candidate a={{ ...action, previewTruncated: true }} />);
  expect(container.textContent).toContain("已截断");
  expect(container.textContent).not.toContain("预览没有显示全部");
  cleanup();
  const { container: whole } = render(<Candidate a={action} />);
  expect(whole.textContent).not.toContain("已截断");
});

it("a cut says what is not shown and never claims the plan is complete", () => {
  const { container } = render(<Candidate a={{ ...action, previewTruncated: true }} />);
  expect(container.textContent).not.toContain("完整");
});

const skill = (n: number): PluginAction => ({ kind: "skill", name: `s${n}`, action: "copy_skill", status: "planned", riskLevel: "high" });

it("market confirmation counts the skills that will be installed, not the rows it was sent", async () => {
  const shown = Array.from({ length: 50 }, (_, i) => skill(i));
  draw(plan({ actions: shown, kinds: { skill: 200, mcp: 0, plugin: 0 }, hiddenActions: 0, previewTruncated: true }));
  expect(screen.getByText("这个来源包含 200 个技能，会全部安装")).toBeTruthy();
  expect(screen.getByText("我已看过这 200 个技能，全部安装", { exact: false })).toBeTruthy();
  expect(screen.getByText(/还有 150 个技能未显示/)).toBeTruthy();
});

it("market confirmation lists hidden steps by count and keeps the high group in view", () => {
  const low: PluginAction = { kind: "plugin", name: "t", action: "install_plugin_package", status: "planned", riskLevel: "medium" };
  const { container } = draw(plan({ actions: [low, action], kinds: { skill: 0, mcp: 0, plugin: 61 }, hiddenActions: 10, previewTruncated: true }));
  expect(container.querySelector('[data-lvl="high"]')).toBeTruthy();
  expect(screen.getByTestId("preview-cut").textContent).toContain("另有 10 项未显示");
});

it("the add-plugin confirmation shows the plan-level cut", async () => {
  const port = new MockPort() as unknown as AgentPort;
  const cut: PluginPlan = { ok: true, status: "planned", applied: false, planId: "p", actions: [action], previewTruncated: true, hiddenActions: 3 };
  vi.spyOn(port, "planPlugin").mockResolvedValue(cut);
  render(<AddPlugin port={port} source="https://github.com/demo/kit" onClose={() => {}} onInstalled={() => {}} />);
  await userEvent.click(await screen.findByRole("button", { name: "查看内容" }));
  expect((await screen.findByTestId("preview-cut")).textContent).toContain("另有 3 项未显示");
});

it("hidden steps alone do not claim any text was cut", () => {
  render(<PreviewCut hidden={30} />);
  const text = screen.getByTestId("preview-cut").textContent ?? "";
  expect(text).toContain("另有 30 项未显示");
  expect(text).not.toContain("不可见字符");
});

it("cut text alone does not claim steps are missing", () => {
  render(<PreviewCut shown />);
  const text = screen.getByTestId("preview-cut").textContent ?? "";
  expect(text).toContain("不可见字符");
  expect(text).not.toContain("另有");
});

it("both are said when both happened", () => {
  render(<PreviewCut shown hidden={2} />);
  const text = screen.getByTestId("preview-cut").textContent ?? "";
  expect(text).toContain("另有 2 项未显示");
  expect(text).toContain("不可见字符");
});
