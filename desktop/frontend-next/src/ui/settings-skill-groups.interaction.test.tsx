// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { Settings } from "./Settings";
import { MockPort } from "../port/mock";
import { MockHub } from "../port/mock_hub";
import type { SkillEntry } from "../port/port";

afterEach(() => { cleanup(); vi.restoreAllMocks(); });

function draw(port: MockPort) {
  return render(<Settings
    hub={new MockHub()} onError={() => {}} port={port} status={null} theme="light" reloadThemes={() => {}}
    onTheme={() => {}} contrast="" onContrast={() => {}} weight="" onWeight={() => {}}
    look={{}} onLook={() => {}} onClose={() => {}} onChanged={() => {}} at="ext:installed" account={null}
    accountUnread="" reloadAccount={() => {}}
  />);
}

const inventory: SkillEntry[] = [
  { name: "builtin-one", scope: "builtin", enabled: true },
  { name: "project-one", scope: "project", enabled: true },
  { name: "mine-one", scope: "global", enabled: true, switchScope: "project" },
  { name: "builtin-two", scope: "builtin", enabled: false },
  { name: "mine-two", scope: "global", enabled: false },
  { name: "package-skill", scope: "plugin", plugin: "review-kit", enabled: true },
];

it("groups the mixed skill catalog by source and retains disabled rows and catalog order within a group", async () => {
  const port = new MockPort();
  vi.spyOn(port, "skills").mockResolvedValue({ implicit: true, skills: inventory });
  draw(port);
  await screen.findByText("mine-one");
  const skills = document.querySelector<HTMLElement>("#set-skills")!;
  const groups = within(skills).getAllByRole("region");
  expect(groups.map((group) => within(group).getByRole("heading").textContent)).toEqual(["我的", "项目", "内置"]);
  expect(groups.map((group) => Array.from(group.querySelectorAll(".nm"), (row) => row.textContent))).toEqual([
    ["mine-one", "mine-two"], ["project-one"], ["builtin-one", "builtin-two"],
  ]);
  expect(within(skills).queryByText("package-skill")).toBeNull();
  expect(skills.querySelector(".now")?.textContent).toBe("3/5 已启用");
});

it("keeps source grouping independent of project overrides and preserves the skill switch and refresh", async () => {
  const port = new MockPort();
  let skills = inventory.map((skill) => ({ ...skill }));
  const read = vi.spyOn(port, "skills").mockImplementation(async () => ({ implicit: false, skills }));
  const toggle = vi.spyOn(port, "setSkillEnabled").mockImplementation(async (name, enabled) => {
    skills = skills.map((skill) => skill.name === name ? { ...skill, enabled } : skill);
  });
  draw(port);
  await screen.findByText("mine-one");
  const mine = screen.getByRole("region", { name: "我的" });
  expect(within(mine).getByRole("button", { name: "仅本项目" })).toBeTruthy();
  expect(mine.textContent).toContain("调不到");
  const control = within(mine).getByRole("switch", { name: "关闭 mine-one" });
  control.focus();
  await userEvent.keyboard("{Enter}");
  expect(toggle).toHaveBeenCalledExactlyOnceWith("mine-one", false, "project", undefined);
  await waitFor(() => expect(read).toHaveBeenCalledTimes(2));
  expect(await within(mine).findByRole("switch", { name: "启用 mine-one" })).toBeTruthy();
  expect(document.activeElement).toBe(control);
  expect(document.querySelector("#set-skills .now")?.textContent).toBe("2/5 已启用");
});

it("shows nonempty custom and unlabelled sources without assigning them to mine or builtin", async () => {
  const port = new MockPort();
  vi.spyOn(port, "skills").mockResolvedValue({ implicit: true, skills: [
    { name: "custom-one", scope: "custom", enabled: true },
    { name: "unlabelled-one", enabled: true },
    { name: "compat-one", scope: "codex_compat", enabled: true },
  ] });
  draw(port);
  await screen.findByText("custom-one");
  const pane = within(document.querySelector<HTMLElement>("#set-skills")!);
  expect(within(pane.getByRole("region", { name: "自定义" })).getByText("custom-one")).toBeTruthy();
  expect(within(pane.getByRole("region", { name: "其他" })).getByText("unlabelled-one")).toBeTruthy();
  expect(within(pane.getByRole("region", { name: "codex_compat" })).getByText("compat-one")).toBeTruthy();
  expect(pane.queryByRole("region", { name: "我的" })).toBeNull();
  expect(pane.queryByRole("region", { name: "内置" })).toBeNull();
});
