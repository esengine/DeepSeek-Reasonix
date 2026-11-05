// @vitest-environment jsdom
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import "./testkit";
import { SkillRow } from "./SkillRow";
import { boot, STORAGE } from "../i18n";
import type { AgentPort, SkillEntry } from "../port/port";

beforeEach(() => {
  localStorage.setItem(STORAGE, "zh");
  boot();
});
afterEach(cleanup);

const sk = (source?: SkillEntry["source"]): SkillEntry => ({ name: "mine", slashName: "mine", description: "d", scope: "global", enabled: true, source });

function draw(entry: SkillEntry) {
  render(<SkillRow sk={entry} implicit port={{} as AgentPort} onDone={vi.fn()} root="" onFailed={vi.fn()} />);
}

it.each([
  ["source_changed", "源文件夹有更新"],
  ["local_edited", "副本被改过"],
  ["diverged", "两边都改过"],
  ["source_missing", "源文件夹不见了"],
] as const)("marks an installed copy that is %s, and says which folder it came from", (state, text) => {
  draw(sk({ path: "/home/me/skills/mine", mode: "copy", state }));
  const mark = screen.getByText(text);
  expect(mark.getAttribute("title")).toContain("/home/me/skills/mine");
});

it.each(["synced", "unknown"] as const)("draws nothing for a copy that is %s", (state) => {
  draw(sk({ path: "/p", mode: "copy", state }));
  expect(document.querySelector("[data-drift]")).toBeNull();
});

it("draws nothing for a skill the installer never recorded", () => {
  draw(sk());
  expect(document.querySelector("[data-drift]")).toBeNull();
});

it.each([
  ["source_changed", "copy", true],
  ["local_edited", "copy", true],
  ["diverged", "copy", true],
  ["synced", "copy", false],
  ["source_missing", "copy", false],
  ["source_changed", "link", false],
] as const)("offers the update for a %s %s install: %s", (state, mode, offered) => {
  draw(sk({ path: "/p", mode, state }));
  const more = screen.queryByRole("button", { name: /^更多/ });
  if (more) fireEvent.click(more);
  expect(screen.queryByRole("menuitem", { name: "从源文件夹更新…" }) !== null).toBe(offered);
});
