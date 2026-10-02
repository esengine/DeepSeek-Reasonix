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
function draw(ref = "", list = models, effective?: string) {
  const onRole = vi.fn();
  render(<ModelUsage models={list} roles={{ ...roles, web_search: ref, web_search_effective: effective }} main="chat/plain" busy="" protocol={{}} onMain={() => {}} onRole={onRole} />);
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
  draw("search/native", models, "project/model");
  expect((screen.getByRole("combobox", { name: "网页搜索" }) as HTMLSelectElement).value).toBe("search/native");
  expect(screen.getByText("项目配置覆盖：实际使用 project/model；此处保存的是全局设置。")).toBeTruthy();
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
it("renders the search empty state even with no configured models", () => {
  draw("lost/model", []);
  expect(screen.getByRole("combobox", { name: "网页搜索" })).toBeTruthy();
  expect(screen.getByText("尚无可用搜索模型")).toBeTruthy();
});
