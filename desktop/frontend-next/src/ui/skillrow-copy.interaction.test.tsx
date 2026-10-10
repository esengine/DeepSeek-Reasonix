// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { act, cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { SkillRow } from "./SkillRow";
import { boot, STORAGE, t } from "../i18n";
import type { AgentPort, SkillEntry } from "../port/port";

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  localStorage.setItem(STORAGE, "zh");
  boot();
});

function draw(over: Partial<SkillEntry> = {}, lang = "zh") {
  localStorage.setItem(STORAGE, lang);
  boot();
  const user = userEvent.setup();
  const write = vi.spyOn(navigator.clipboard, "writeText").mockResolvedValue();
  const setSkillEnabled = vi.fn().mockResolvedValue(undefined);
  const clearSkillOverride = vi.fn().mockResolvedValue(undefined);
  const port = { setSkillEnabled, clearSkillOverride } as unknown as AgentPort;
  const sk: SkillEntry = { name: "package-owned-review", slashName: "review-kit:review", plugin: "review-kit", enabled: true, ...over };
  const props = { sk, port, implicit: true, root: "/w/repo", onDone: vi.fn(), onFailed: vi.fn() };
  const view = render(<SkillRow {...props} />);
  return { user, write, setSkillEnabled, clearSkillOverride, props, view };
}

const copy = (command = "/review-kit:review") => screen.getByRole("button", { name: t("复制调用命令 {command}", { command }) });

it.each(["zh", "en"])("copies the canonical namespaced invocation with keyboard feedback (%s)", async (lang) => {
  const { user, write, setSkillEnabled, props } = draw({}, lang);
  await user.tab();
  expect(document.activeElement).toBe(copy());
  await user.keyboard("{Enter}");
  expect(write).toHaveBeenCalledWith("/review-kit:review");
  expect(await screen.findByText(t("已复制"))).toBeTruthy();
  expect(copy().getAttribute("title")).toBe(t("已复制"));
  expect(setSkillEnabled).not.toHaveBeenCalled();
  expect(props.onDone).not.toHaveBeenCalled();
  expect(props.onFailed).not.toHaveBeenCalled();
  await user.tab();
  expect(document.activeElement).toBe(screen.getByRole("switch"));
});

it("reports a denied clipboard locally and allows a retry without changing the skill", async () => {
  const { user, write, props, setSkillEnabled } = draw();
  write.mockRejectedValueOnce(new Error("clipboard denied"));
  await user.click(copy());
  expect(await screen.findByText(t("复制失败，请重试"))).toBeTruthy();
  expect(props.onFailed).not.toHaveBeenCalled();
  expect(screen.getByRole("switch").getAttribute("aria-checked")).toBe("true");
  await user.click(copy());
  expect(await screen.findByText(t("已复制"))).toBeTruthy();
  expect(write).toHaveBeenCalledTimes(2);
  expect(setSkillEnabled).not.toHaveBeenCalled();
});

it("lets a disabled read-only skill's invocation be copied without enabling it or clearing its override", async () => {
  const { user, write, setSkillEnabled, clearSkillOverride } = draw({ enabled: false, readOnly: true, switchScope: "project" });
  await user.click(copy());
  expect(write).toHaveBeenCalledWith("/review-kit:review");
  expect(screen.getByRole("switch").getAttribute("aria-checked")).toBe("false");
  expect(screen.getByText(t("只读"))).toBeTruthy();
  expect(setSkillEnabled).not.toHaveBeenCalled();
  expect(clearSkillOverride).not.toHaveBeenCalled();
});

it("does not invent an invocation for a model-only skill", () => {
  draw({ slashName: undefined });
  expect(screen.queryByRole("button", { name: /复制调用命令/ })).toBeNull();
  expect(screen.getByText(t("只能模型自选"))).toBeTruthy();
  expect(screen.getByRole("switch")).toBeTruthy();
});

it("keeps copying independent of a pending enable operation", async () => {
  const { user, write, setSkillEnabled, props } = draw();
  let finish!: () => void;
  setSkillEnabled.mockImplementationOnce(() => new Promise<void>((resolve) => { finish = resolve; }));
  await user.click(screen.getByRole("switch"));
  await user.click(copy());
  expect(write).toHaveBeenCalledWith("/review-kit:review");
  expect(screen.getByRole<HTMLButtonElement>("switch").disabled).toBe(true);
  expect(props.onDone).not.toHaveBeenCalled();
  await act(async () => finish());
  expect(props.onDone).toHaveBeenCalledTimes(1);
  expect(screen.getByRole<HTMLButtonElement>("switch").disabled).toBe(false);
});

it("does not carry a previous invocation's pending copy feedback into refreshed inventory", async () => {
  const { user, write, view, props } = draw();
  let finish!: () => void;
  write.mockImplementationOnce(() => new Promise<void>((resolve) => { finish = resolve; }));
  await user.click(copy());
  view.rerender(<SkillRow {...props} sk={{ ...props.sk, slashName: "review-kit:audit" }} />);
  await act(async () => finish());
  expect(screen.queryByText(t("已复制"))).toBeNull();
  expect(copy("/review-kit:audit").getAttribute("title")).toBe(t("复制调用命令 {command}", { command: "/review-kit:audit" }));
  await user.click(copy("/review-kit:audit"));
  expect(write).toHaveBeenLastCalledWith("/review-kit:audit");
});
