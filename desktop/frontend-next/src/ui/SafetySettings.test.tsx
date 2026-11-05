// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import "./testkit";
import { MockPort } from "../port/mock";
import type { AgentPort } from "../port/port";
import { SafetySettings } from "./SafetySettings";
import { SettingDeepContext } from "./SettingDeep";

afterEach(cleanup);

function draw(mode: "ask" | "yolo" = "ask") {
  const go = vi.fn();
  const onMode = vi.fn();
  render(
    <SettingDeepContext.Provider value={{ pages: true, open: null, section: "权限与安全", go }}>
      <SafetySettings port={new MockPort() as unknown as AgentPort} mode={mode} busy="" onMode={onMode} onChanged={() => {}} />
    </SettingDeepContext.Provider>,
  );
  return { go, onMode };
}

it("is three cards, every block one row with its scope and when it takes effect", async () => {
  draw();
  expect([...document.querySelectorAll(".prefs-card > header h3")].map((h) => h.textContent)).toEqual(["批准", "沙箱与执行", "联网"]);
  for (const id of ["approval", "rules", "external-origin", "tool-list", "sandbox", "shell", "write-lease", "web-access", "browser-tools"]) {
    await waitFor(() => expect(document.querySelector(`.prefs-card > [data-setting="${id}"], .prefs-card [data-setting="${id}"]`), id).toBeTruthy());
  }
  expect(document.querySelector('[data-setting="approval"] .apply')?.textContent).toContain("本机");
});

it("picks an approval mode from the row and opens pages from pills, counting rules and tools", async () => {
  const { go, onMode } = draw();
  fireEvent.click(screen.getByRole("radio", { name: "自动批准" }));
  expect(onMode).toHaveBeenCalledWith("auto");
  const rules = await screen.findByRole("button", { name: /^编辑规则（\d+ 条）$/ });
  fireEvent.click(rules);
  expect(go).toHaveBeenLastCalledWith("rules");
  fireEvent.click(await screen.findByRole("button", { name: /^查看与调整（\d+）$/ }));
  expect(go).toHaveBeenLastCalledWith("tool-list");
});

it("says what the permissive mode does only while it is picked", () => {
  draw("ask");
  expect(document.querySelector('[role="status"].note')).toBeNull();
  cleanup();
  draw("yolo");
  expect(document.querySelector('[role="status"].note')).toBeTruthy();
});
