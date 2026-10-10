// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { ModelUsage } from "./ModelUsage";
import type { ModelEntry, RoleAssignments } from "../port/port";

afterEach(cleanup);
const roles: RoleAssignments = { planner: "", subagent: "", guardian: "", vision: "", decision: "", web_search: "" };
const models: ModelEntry[] = [
  { ref: "chat/plain", provider: "chat", model: "plain" },
  { ref: "search/native", provider: "search", model: "native", webSearch: true },
];
function draw(ref = "", list = models, extra: Partial<RoleAssignments> = {}) {
  const onRole = vi.fn();
  render(<ModelUsage models={list} roles={{ ...roles, web_search: ref, ...extra }} main="chat/plain" busy="" protocol={{}} onMain={() => {}} onRole={onRole} />);
  return onRole;
}
it("offers automatic search and only kernel-declared search models", async () => {
  const onRole = draw();
  const select = screen.getByRole("combobox", { name: "网页搜索" });
  expect(select.textContent).toContain("自动选择");
  expect(select.textContent).toContain("native");
  expect(select.textContent).not.toContain("plain");
  await userEvent.selectOptions(select, "search/native");
  expect(onRole).toHaveBeenCalledWith("web_search", "search/native");
  await userEvent.selectOptions(select, "");
  expect(onRole).toHaveBeenCalledWith("web_search", "");
});
it("shows a project override without replacing the stored global selection", () => {
  draw("search/native", models, { web_search_effective: "chat/plain", web_search_source: "project" });
  expect((screen.getByRole("combobox", { name: "网页搜索" }) as HTMLSelectElement).value).toBe("search/native");
  expect(screen.getByText("项目配置覆盖：实际使用 chat；此处保存的是全局设置。")).toBeTruthy();
});
it("offers native search routes even when the account displays its chat protocol", () => {
  draw("", [
    { ref: "chat/plain", provider: "chat", model: "plain", kind: "openai", vendor: "fixture.example", keyEnv: "FIXTURE_KEY" },
    { ref: "search/native", provider: "search", model: "native", kind: "anthropic", vendor: "fixture.example", keyEnv: "FIXTURE_KEY", webSearch: true },
  ]);
  expect(screen.getByRole("combobox", { name: "网页搜索" }).textContent).toContain("native");
});
it("keeps an unavailable saved assignment visible until the user clears it", () => {
  draw("lost/model");
  const select = screen.getByRole("combobox", { name: "网页搜索" }) as HTMLSelectElement;
  expect(select.value).toBe("lost/model");
  expect(select.textContent).toContain("不可用");
});
it("explains an empty search catalog and allows clearing an unavailable choice", () => {
  draw("lost/model", [models[0]]);
  const select = screen.getByRole("combobox", { name: "网页搜索" }) as HTMLSelectElement;
  expect(select.disabled).toBe(false);
  expect(screen.getByText("尚无可用搜索模型")).toBeTruthy();
});
it("normalizes the persisted auto assignment for the automatic option", () => {
  draw("auto");
  expect((screen.getByRole("combobox", { name: "网页搜索" }) as HTMLSelectElement).value).toBe("");
});
it("keeps the catalog-unreadable state when no model is listed", () => {
  draw("lost/model", []);
  expect(screen.getByText("无法读取模型列表。")).toBeTruthy();
  expect(screen.queryByRole("combobox", { name: "网页搜索" })).toBeNull();
});
it("names the account that automatic search really uses", () => {
  draw("", models, { web_search_effective: "search/native" });
  expect(screen.getByText("自动：使用对话模型自带的搜索（search）")).toBeTruthy();
});
it("says so when automatic search has nothing to use", () => {
  draw("", models, { web_search_effective: "" });
  expect(screen.getByText("对话模型没有内置搜索；需要联网搜索时请选择一个搜索模型")).toBeTruthy();
});
it("states the reason code the kernel reports for an unusable assignment", () => {
  draw("lost/model", models, { web_search_reason: "no_credentials" });
  expect(screen.getByText("搜索模型所在的连接没有凭据")).toBeTruthy();
});
it("disables the row while a turn runs", () => {
  render(<ModelUsage models={models} roles={{ ...roles, web_search: "" }} main="chat/plain" busy="running" protocol={{}} onMain={() => {}} onRole={vi.fn()} />);
  expect((screen.getByRole("combobox", { name: "网页搜索" }) as HTMLSelectElement).disabled).toBe(true);
});
