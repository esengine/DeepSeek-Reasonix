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

it("is three cards of single rows, plan mode and the display switches living elsewhere", async () => {
  draw();
  expect([...document.querySelectorAll(".prefs-card > header h3")].map((h) => h.textContent)).toEqual(["人设与预设", "计划与提问", "协作"]);
  for (const id of ["persona", "agent-presets", "plan-approval", "ask-timeout", "turn-narration", "progress-watch", "session-ref", "subagent-claims", "peers"]) {
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
