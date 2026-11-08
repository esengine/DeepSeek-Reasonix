// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import "./testkit";
import { MockPort } from "../port/mock";
import type { AgentPort } from "../port/port";
import { AgentSettings } from "./AgentSettings";
import { SettingDeepContext } from "./SettingDeep";

afterEach(cleanup);

function draw(port = new MockPort()) {
  const go = vi.fn();
  render(
    <SettingDeepContext.Provider value={{ pages: true, open: null, section: "Agent 行为", go }}>
      <AgentSettings port={port as unknown as AgentPort} hub={{ tree: async () => [] }} persona={false} onChanged={() => {}} />
    </SettingDeepContext.Provider>,
  );
  return go;
}

it("shows every group directly without an extra navigation row", async () => {
  draw();
  const names = ["计划与提问", "人设与预设", "协作", "自动化", "运行监测"];
  expect([...document.querySelectorAll(".prefs-card > header h3")].map((h) => h.textContent)).toEqual(names);
  expect(screen.queryByRole("navigation", { name: "本页包含" })).toBeNull();
  expect(screen.queryByText("更多行为设置")).toBeNull();
  for (const id of ["persona", "agent-presets", "plan-approval", "ask-timeout", "turn-narration", "progress-watch", "session-ref", "subagent-claims", "peers", "hooks", "scheduled-runs"]) {
    await waitFor(() => expect(document.querySelector(`[data-setting="${id}"]`), id).toBeTruthy());
  }
  for (const id of ["plan-mode", "changed-files", "fold-work", "feedback-level-notices"]) expect(document.querySelector(`[data-setting="${id}"]`), id).toBeNull();
  for (const id of ["progress-watch", "session-ref", "subagent-claims", "peers", "hooks", "scheduled-runs"]) {
    expect(document.querySelector(`[data-setting="${id}"]`)?.closest("details")).toBeNull();
  }
  expect(document.querySelector('#set-persona .scope-tag')?.textContent).toBe("下次会话构建时生效");
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
