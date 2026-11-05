// @vitest-environment jsdom
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { SkillRow } from "./SkillRow";
import { boot, STORAGE } from "../i18n";
import { HttpError } from "../port/port";
import type { AgentPort, SkillEntry } from "../port/port";

beforeEach(() => {
  localStorage.setItem(STORAGE, "zh");
  boot();
});
afterEach(cleanup);

const mine: SkillEntry = { name: "helper", enabled: true, scope: "global", path: "/home/u/.reasonix/skills/helper/SKILL.md", removes: "/home/u/.reasonix/skills/helper", removesFolder: true, description: "a helper" };

function draw(sk: Partial<SkillEntry> = {}, root = "", removeSkill = vi.fn().mockResolvedValue(undefined)) {
  const port = { removeSkill, setSkillEnabled: vi.fn(), clearSkillOverride: vi.fn() } as unknown as AgentPort;
  const onDone = vi.fn();
  const onFailed = vi.fn();
  render(<SkillRow sk={{ ...mine, ...sk }} implicit port={port} root={root} onDone={onDone} onFailed={onFailed} />);
  return { removeSkill, onDone, onFailed, user: userEvent.setup() };
}

const open = () => {
  const more = screen.queryByRole("button", { name: /^更多/ });
  if (more && !screen.queryByRole("menu")) fireEvent.click(more);
  return screen.queryByRole("menuitem", { name: "卸载…" });
};

it("offers removal for a skill of the person's own and for none of the others", () => {
  draw();
  expect(open()).not.toBeNull();
  cleanup();
  draw({ scope: "builtin", path: "(builtin)", removes: undefined });
  expect(open()).toBeNull();
  cleanup();
  draw({ plugin: "review-kit", removes: undefined });
  expect(open()).toBeNull();
  cleanup();
  draw({ path: undefined, removes: undefined });
  expect(open()).toBeNull();
  cleanup();
  draw({}, "/w/another-project");
  expect(open()).toBeNull();
});

it("says a file, not a folder, when the skill is one file", async () => {
  const { user } = draw({ removes: "/home/u/.reasonix/skills/helper.md", removesFolder: false });
  await user.click(open()!);
  expect(screen.getByText("/home/u/.reasonix/skills/helper.md")).toBeTruthy();
  expect(screen.getByText(/删除下面这个文件，/)).toBeTruthy();
});

it("names the place that will be deleted and removes nothing until the person says so", async () => {
  const { user, removeSkill } = draw();
  await user.click(open()!);
  expect(screen.getByText("/home/u/.reasonix/skills/helper")).toBeTruthy();
  expect(screen.getByText(/这个文件夹及其中所有文件/)).toBeTruthy();
  expect(screen.getByText("卸载技能「helper」？")).toBeTruthy();
  expect(removeSkill).not.toHaveBeenCalled();
  await user.click(screen.getByRole("button", { name: "取消" }));
  expect(screen.queryByText("卸载技能「helper」？")).toBeNull();
  expect(removeSkill).not.toHaveBeenCalled();
});

it("removes the skill that was shown, by its scope and path, and refreshes the list", async () => {
  const { user, removeSkill, onDone } = draw();
  await user.click(open()!);
  await user.click(screen.getByRole("button", { name: "卸载" }));
  expect(removeSkill).toHaveBeenCalledWith("helper", "global", "/home/u/.reasonix/skills/helper/SKILL.md", undefined);
  await waitFor(() => expect(onDone).toHaveBeenCalled());
});

it("says so when the list was stale, deletes nothing more, and offers to read it again", async () => {
  const removeSkill = vi.fn().mockRejectedValue(new HttpError(409, "changed", { code: "skill.changed" }));
  const { user, onDone } = draw({}, "", removeSkill);
  await user.click(open()!);
  await user.click(screen.getByRole("button", { name: "卸载" }));
  expect(await screen.findByRole("alert")).toBeTruthy();
  expect(onDone).not.toHaveBeenCalled();
  await user.click(screen.getByRole("button", { name: "重新读取" }));
  expect(onDone).toHaveBeenCalled();
});
