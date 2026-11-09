// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import "./testkit";
import { MockPort } from "../port/mock";
import type { AgentPort } from "../port/port";
import { SafetySettings } from "./SafetySettings";

afterEach(cleanup);

function draw(mode: "ask" | "yolo" = "ask") {
  const onMode = vi.fn();
  render(
    <>
      <SafetySettings port={new MockPort() as unknown as AgentPort} mode={mode} busy="" onMode={onMode} onChanged={() => {}} />
    </>,
  );
  return { onMode };
}

it("is three cards, every block one row with its scope and when it takes effect", async () => {
  draw();
  expect([...document.querySelectorAll(".prefs-card > header h3")].map((h) => h.textContent)).toEqual(["批准", "沙箱与执行", "联网"]);
  for (const id of ["approval", "rules", "external-origin", "tool-list", "sandbox", "shell", "write-lease", "web-access", "browser-tools"]) {
    await waitFor(() => expect(document.querySelector(`.prefs-card > [data-setting="${id}"], .prefs-card [data-setting="${id}"]`), id).toBeTruthy());
  }
  expect(document.querySelector('[data-setting="approval"] .apply')?.textContent).toContain("本机");
});

it("picks an approval mode while rules and tool controls stay expanded", async () => {
  const { onMode } = draw();
  fireEvent.click(screen.getByRole("radio", { name: "自动批准" }));
  expect(onMode).toHaveBeenCalledWith("auto");
  await waitFor(() => {
    expect(document.querySelector('#set-rules [data-action="permissions.recipe"]')).toBeTruthy();
    expect(document.querySelector('#set-tool-list button')).toBeTruthy();
  });
});

it("says what the permissive mode does only while it is picked", () => {
  draw("ask");
  expect(document.querySelector('[role="status"].note')).toBeNull();
  cleanup();
  draw("yolo");
  expect(document.querySelector('[role="status"].note')).toBeTruthy();
});
