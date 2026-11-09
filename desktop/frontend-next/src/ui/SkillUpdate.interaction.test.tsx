// @vitest-environment jsdom
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { SkillUpdate } from "./SkillUpdate";
import { boot, STORAGE } from "../i18n";
import { HttpError } from "../port/port";
import type { AgentPort, SkillUpdatePlan } from "../port/port";

beforeEach(() => {
  localStorage.setItem(STORAGE, "zh");
  boot();
});
afterEach(cleanup);

const plan = (over: Partial<SkillUpdatePlan> = {}): SkillUpdatePlan => ({
  name: "mine", source: "/home/me/skills/mine", state: "source_changed", overwritesLocal: false, planId: "p1",
  changes: [
    { path: "scripts/run.sh", kind: "changed", code: true, diff: "-echo one\n+echo two\n" },
    { path: "SKILL.md", kind: "changed", code: false, diff: "-v1\n+v2\n" },
  ],
  ...over,
});

function draw(p: SkillUpdatePlan, apply = vi.fn().mockResolvedValue(undefined)) {
  const port = { planSkillUpdate: vi.fn().mockResolvedValue(p), applySkillUpdate: apply } as unknown as AgentPort;
  const onDone = vi.fn();
  const onClose = vi.fn();
  render(<SkillUpdate port={port} name="mine" onClose={onClose} onDone={onDone} />);
  return { port, apply, onDone, onClose };
}

const update = () => screen.getByRole("button", { name: "更新" }) as HTMLButtonElement;

it("will not update until every script has been opened, then applies the plan that was shown", async () => {
  const { apply, onDone } = draw(plan());
  await screen.findByText("更新 mine：来源文件夹有 2 处变化");
  expect(update().disabled).toBe(true);
  expect(screen.getByText("还有 1 个文件没有展开")).toBeTruthy();
  expect(screen.getByText("scripts/run.sh").closest("summary")?.querySelector(".lucide-pencil")).not.toBeNull();
  await userEvent.click(screen.getByText("scripts/run.sh"));
  expect(screen.getByText(/\+echo two/)).toBeTruthy();
  expect(update().disabled).toBe(false);
  await userEvent.click(update());
  expect(apply).toHaveBeenCalledExactlyOnceWith("mine", "p1", false);
  expect(onDone).toHaveBeenCalled();
});

it("asks for a confirmation before overwriting local edits and sends it", async () => {
  const { apply } = draw(plan({ overwritesLocal: true, changes: [{ path: "SKILL.md", kind: "changed", code: false, diff: "-a\n+b\n" }] }));
  await screen.findByText("已安装的副本在安装后被改过");
  expect(update().disabled).toBe(true);
  await userEvent.click(screen.getByRole("checkbox", { name: "我知道会覆盖本地改动" }));
  await userEvent.click(update());
  expect(apply).toHaveBeenCalledExactlyOnceWith("mine", "p1", true);
});

it("says a file it cannot show must be read in the source folder, and holds until the person says they did", async () => {
  draw(plan({ changes: [{ path: "scripts/big.sh", kind: "added", code: true, truncated: true }] }));
  await screen.findByText("scripts/big.sh");
  expect(update().disabled).toBe(true);
  await userEvent.click(screen.getByText("scripts/big.sh"));
  expect(screen.getByText(/文件太大，这里不显示内容/).textContent).toContain("/home/me/skills/mine");
  await userEvent.click(screen.getByRole("checkbox", { name: /无法显示，我已在来源文件夹里看过/ }));
  expect(update().disabled).toBe(false);
});

it("offers to review again when the plan went stale, and loads the new one", async () => {
  const apply = vi.fn().mockRejectedValue(new HttpError(409, "stale", { code: "skills.update_plan_stale" }, true));
  const { port } = draw(plan({ changes: [{ path: "SKILL.md", kind: "changed", code: false, diff: "-a\n+b\n" }] }), apply);
  await screen.findByText(/来源文件夹有 1 处变化/);
  await userEvent.click(update());
  expect(await screen.findByText(/又变了/)).toBeTruthy();
  await userEvent.click(screen.getByRole("button", { name: "重新查看" }));
  expect(port.planSkillUpdate).toHaveBeenCalledTimes(2);
});

it("says there is nothing to update when the copy already matches", async () => {
  draw(plan({ changes: [], state: "synced" }));
  expect(await screen.findByText("mine 与来源文件夹的内容一致，没有可更新的内容")).toBeTruthy();
  expect(update().disabled).toBe(true);
});

it("lets the person bring the record up to date when the copy already matches but the record is behind", async () => {
  const { apply } = draw(plan({ changes: [], state: "diverged", overwritesLocal: true }));
  expect(await screen.findByText("mine 的内容已与来源文件夹一致，只是安装记录还停在旧版本")).toBeTruthy();
  await userEvent.click(screen.getByRole("checkbox", { name: "我知道会覆盖本地改动" }));
  await userEvent.click(update());
  expect(apply).toHaveBeenCalledExactlyOnceWith("mine", "p1", true);
});

it("shows why it could not read the plan, with a way to close", async () => {
  const port = { planSkillUpdate: vi.fn().mockRejectedValue(new HttpError(409, "gone", { code: "skills.update_source_missing" }, true)) } as unknown as AgentPort;
  const onClose = vi.fn();
  render(<SkillUpdate port={port} name="mine" onClose={onClose} onDone={vi.fn()} />);
  expect(await screen.findByText(/找不到来源文件夹/)).toBeTruthy();
  await userEvent.click(screen.getByRole("button", { name: "关闭" }));
  expect(onClose).toHaveBeenCalled();
});
