// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { AddPlugin, Candidate } from "./AddPlugin";
import { PlanConfirm } from "./MarketConfirm";
import { MockPort } from "../port/mock";
import { boot, STORAGE } from "../i18n";
import type { AgentPort, PluginAction, PluginPlan } from "../port/port";

const clipboard = Object.getOwnPropertyDescriptor(navigator, "clipboard");
afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  if (clipboard) Object.defineProperty(navigator, "clipboard", clipboard);
  else Reflect.deleteProperty(navigator, "clipboard");
  localStorage.setItem(STORAGE, "zh");
  boot();
});

const source = "https://github.com/demo/review-kit/tree/release/candidate";
const commit = "a123456789abcdef0123456789abcdef01234567";
const nextCommit = "b123456789abcdef0123456789abcdef01234567";
const action: PluginAction = {
  kind: "plugin", action: "install_plugin", status: "planned", riskLevel: "medium",
  name: "review-kit", source, commit, version: "2.0.0", skillCount: 1,
};
const plan: PluginPlan = { ok: true, applied: false, status: "planned", source, planId: "review-plan", actions: [action] };

it.each([
  ["zh", false], ["en", false], ["zh", true], ["en", true],
])("copies the resolved commit in a %s preview (updating=%s) without changing confirmation", async (lang, updating) => {
  localStorage.setItem(STORAGE, lang);
  boot();
  const user = userEvent.setup();
  const write = vi.spyOn(navigator.clipboard, "writeText").mockResolvedValue();
  const port = new MockPort() as unknown as AgentPort;
  const preview = vi.spyOn(port, "planPlugin").mockResolvedValue(plan);
  const install = vi.spyOn(port, "installPlugin").mockResolvedValue({ ...plan, applied: true, status: "done" });
  const publish = vi.spyOn(port, "publishMarket");
  const onInstalled = vi.fn();
  render(<AddPlugin port={port} source={source} updating={updating ? { name: "review-kit", source, version: "1.0.0", root: "/plugins/review-kit", enabled: true } : undefined} onClose={() => {}} onInstalled={onInstalled} />);
  if (!updating) await user.click(screen.getByRole("button", { name: lang === "zh" ? "查看内容" : "See what it is" }));
  expect(await screen.findByText(lang === "zh" ? "已解析提交" : "Resolved commit")).toBeTruthy();
  expect(screen.getByText(commit).textContent).toBe(commit);
  await user.click(screen.getByRole("button", { name: lang === "zh" ? "复制提交号" : "Copy commit" }));
  await waitFor(() => expect(write).toHaveBeenCalledExactlyOnceWith(commit));
  expect(screen.getByText(lang === "zh" ? "已复制" : "Copied")).toBeTruthy();
  expect(install).not.toHaveBeenCalled();
  expect(publish).not.toHaveBeenCalled();
  expect(preview).toHaveBeenCalledExactlyOnceWith({ source, name: updating ? "review-kit" : undefined, replace: updating, planId: undefined });
  await user.click(screen.getByRole("button", { name: lang === "zh" ? (updating ? "更新" : "安装") : (updating ? "Update" : "Install") }));
  await screen.findByRole("button", { name: lang === "zh" ? "完成" : "Done" });
  expect(install).toHaveBeenCalledExactlyOnceWith({ source, name: updating ? "review-kit" : undefined, replace: updating, planId: plan.planId });
  expect(onInstalled).toHaveBeenCalledTimes(1);
});

it.each([
  ["zh", false], ["en", false], ["zh", true], ["en", true],
])("copies the same producer value in the %s market confirmation (own=%s)", async (lang, own) => {
  localStorage.setItem(STORAGE, lang);
  boot();
  const user = userEvent.setup();
  const write = vi.spyOn(navigator.clipboard, "writeText").mockResolvedValue();
  const onInstall = vi.fn();
  const onCancel = vi.fn();
  render(<PlanConfirm slug="demo/review-kit" plan={{ ...plan, slug: "demo/review-kit", version: "2.0.0", unreviewed: own, contentDigest: "preview-digest" }} own={own} busy={false} error="" onInstall={onInstall} onCancel={onCancel} />);
  expect(screen.getByText(lang === "zh" ? "已解析提交" : "Resolved commit")).toBeTruthy();
  expect(screen.getByText(commit).textContent).toBe(commit);
  await user.click(screen.getByRole("button", { name: lang === "zh" ? "复制提交号" : "Copy commit" }));
  await waitFor(() => expect(write).toHaveBeenCalledExactlyOnceWith(commit));
  expect(onInstall).not.toHaveBeenCalled();
  expect(onCancel).not.toHaveBeenCalled();
  if (own) expect(screen.getByText(lang === "zh" ? "未审核 · 仅你可见" : "Unreviewed · visible only to you")).toBeTruthy();
  await user.click(screen.getByRole("button", { name: lang === "zh" ? "安装" : "Install" }));
  expect(onInstall).toHaveBeenCalledTimes(1);
});

it.each([undefined, ""])("does not invent a resolved commit for a local or older response (%s)", (value) => {
  render(<Candidate a={{ ...action, source: "/work/review-kit", commit: value }} />);
  expect(screen.queryByText("已解析提交")).toBeNull();
  expect(screen.queryByRole("button", { name: "复制提交号" })).toBeNull();
  expect(screen.getByText("1 个技能")).toBeTruthy();
});

it.each(["skill", "mcp", "theme"])("does not present a plugin commit on a %s action", (kind) => {
  render(<Candidate a={{ ...action, kind }} />);
  expect(screen.queryByText(commit)).toBeNull();
  expect(screen.queryByRole("button", { name: "复制提交号" })).toBeNull();
});

it("copies each plugin's own commit and reads a changed plan value", async () => {
  const user = userEvent.setup();
  const write = vi.spyOn(navigator.clipboard, "writeText").mockResolvedValue();
  const view = render(<><Candidate a={action} /><Candidate a={{ ...action, name: "other", commit: nextCommit }} /></>);
  const first = within(screen.getByText("review-kit").closest(".cand")!);
  const second = within(screen.getByText("other").closest(".cand")!);
  await user.click(first.getByRole("button", { name: "复制提交号" }));
  await user.click(second.getByRole("button", { name: "复制提交号" }));
  expect(write.mock.calls).toEqual([[commit], [nextCommit]]);
  view.rerender(<Candidate a={{ ...action, commit: nextCommit }} />);
  expect(screen.queryByText(commit)).toBeNull();
  await user.click(screen.getByRole("button", { name: "复制提交号" }));
  await waitFor(() => expect(write).toHaveBeenLastCalledWith(nextCommit));
});

it("reports clipboard refusal and lets the same commit be copied again", async () => {
  const user = userEvent.setup();
  const write = vi.spyOn(navigator.clipboard, "writeText").mockRejectedValueOnce(new Error("clipboard denied")).mockResolvedValue();
  render(<Candidate a={action} />);
  const button = screen.getByRole("button", { name: "复制提交号" });
  await user.click(button);
  await screen.findByText("复制失败，请重试");
  await user.click(button);
  await screen.findByText("已复制");
  expect(write.mock.calls).toEqual([[commit], [commit]]);
});

it("uses the shared fallback when the native host has no clipboard API", async () => {
  const user = userEvent.setup();
  Object.defineProperty(navigator, "clipboard", { configurable: true, value: undefined });
  const exec = vi.fn(() => {
    expect(document.querySelector<HTMLTextAreaElement>("textarea")?.value).toBe(commit);
    return true;
  });
  Object.defineProperty(document, "execCommand", { configurable: true, value: exec });
  try {
    render(<Candidate a={action} />);
    await user.click(screen.getByRole("button", { name: "复制提交号" }));
    await screen.findByText("已复制");
    expect(exec).toHaveBeenCalledExactlyOnceWith("copy");
    expect(document.querySelector("textarea")).toBeNull();
  } finally {
    Reflect.deleteProperty(document, "execCommand");
  }
});
