// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import "./testkit";
import { MockPort } from "../port/mock";
import type { AgentPort } from "../port/port";
import { AgentSettings } from "./AgentSettings";
import { SettingDeepContext } from "./SettingDeep";

afterEach(cleanup);

function draw() {
  const go = vi.fn();
  render(
    <SettingDeepContext.Provider value={{ pages: true, open: null, section: "Agent 行为", go }}>
      <AgentSettings port={new MockPort() as unknown as AgentPort} hub={{ tree: async () => [] }} persona={false} onChanged={() => {}} />
    </SettingDeepContext.Provider>,
  );
  return go;
}

it("is four cards of single rows, plan mode and the display switches living elsewhere", async () => {
  draw();
  expect([...document.querySelectorAll(".prefs-card > header h3")].map((h) => h.textContent)).toEqual(["人设与预设", "计划与提问", "协作", "自动化"]);
  for (const id of ["persona", "agent-presets", "plan-approval", "ask-timeout", "turn-narration", "progress-watch", "session-ref", "subagent-claims", "peers", "hooks", "scheduled-runs"]) {
    await waitFor(() => expect(document.querySelector(`[data-setting="${id}"]`), id).toBeTruthy());
  }
  for (const id of ["plan-mode", "changed-files", "fold-work", "feedback-level-notices"]) expect(document.querySelector(`[data-setting="${id}"]`), id).toBeNull();
});

it("opens the presets page from both the persona row and the presets row", () => {
  const go = draw();
  fireEvent.click(screen.getByRole("button", { name: "编辑人设" }));
  expect(go).toHaveBeenLastCalledWith("agent-presets");
  fireEvent.click(screen.getByRole("button", { name: "管理预设" }));
  expect(go).toHaveBeenLastCalledWith("agent-presets");
});

it("opens the hooks and the scheduled-runs pages from the automation card", async () => {
  const go = draw();
  const card = [...document.querySelectorAll<HTMLElement>(".prefs-card")].find((c) => c.querySelector("header h3")?.textContent === "自动化")!;
  await waitFor(() => expect([...card.querySelectorAll("button")].filter((b) => b.textContent === "管理")).toHaveLength(2));
  const buttons = [...card.querySelectorAll("button")].filter((b) => b.textContent === "管理");
  fireEvent.click(buttons[0]);
  expect(go).toHaveBeenLastCalledWith("hooks");
  fireEvent.click(buttons[1]);
  expect(go).toHaveBeenLastCalledWith("scheduled-runs");
  expect(card.textContent).toContain("在工具调用前后运行你自己的命令");
  expect(card.textContent).toContain("到点让 Agent 只读地跑一遍并留下报告");
});
