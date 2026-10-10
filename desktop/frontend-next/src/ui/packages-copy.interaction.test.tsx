// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, render, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { Packages } from "./Packages";
import { MockPort } from "../port/mock";
import type { AgentPort, PluginPackage } from "../port/port";
import { boot, STORAGE, t } from "../i18n";

afterEach(() => {
  cleanup();
  localStorage.setItem(STORAGE, "zh");
  boot();
  vi.restoreAllMocks();
});

const entries = [
  ["skills", "/review-kit:review"],
  ["commands", "/review-kit:check"],
  ["agents", "/review-kit:agent:reviewer"],
  ["prompts", "/review-kit:notes"],
] as const;

const pkg: PluginPackage = {
  name: "review-kit", root: "/plugins/review-kit", source: "/sources/review-kit", enabled: true,
  skills: [{ name: "review", invocation: entries[0][1], description: "Review source" }],
  commands: [{ name: "check", invocation: entries[1][1], description: "Check source" }],
  agents: [{ name: "reviewer", invocation: entries[2][1], description: "Delegate review" }],
  prompts: [{ name: "notes", invocation: entries[3][1], description: "Prepare notes" }],
  themes: [{ name: "paper", description: "Paper theme" }],
};

async function setup(lang = "zh") {
  localStorage.setItem(STORAGE, lang);
  boot();
  const user = userEvent.setup();
  const copy = vi.spyOn(navigator.clipboard, "writeText").mockResolvedValue();
  const port = new MockPort() as unknown as AgentPort;
  const onChanged = vi.fn();
  const onUpdate = vi.fn();
  const props = { port, onChanged, onUpdate, updating: "" };
  const view = render(<Packages {...props} packages={[pkg]} />);
  const details = view.container.querySelector<HTMLDetailsElement>("details")!;
  await user.click(details.querySelector("summary")!);
  expect(details.open).toBe(true);
  return { user, copy, port, props, view, details, row: within(details) };
}

function label(command: string) {
  return t("复制调用命令 {command}", { command });
}

describe("installed package invocation copy", () => {
  it.each(entries.flatMap(([kind, command]) => [
    ["zh", kind, command], ["en", kind, command],
  ]))("copies the %s %s invocation with the keyboard", async (lang, _kind, command) => {
    const { user, copy, row, details, port, props } = await setup(lang);
    const toggle = vi.spyOn(port, "setPluginEnabled");
    const remove = vi.spyOn(port, "removePlugin");
    const exportPackage = vi.spyOn(port, "exportPlugin");
    const button = row.getByRole("button", { name: label(command) });
    if (lang === "en") expect(button.getAttribute("aria-label")).toBe(`Copy invocation ${command}`);
    button.focus();
    await user.keyboard("{Enter}");
    expect(copy).toHaveBeenCalledExactlyOnceWith(command);
    expect(button.getAttribute("title")).toBe(t("已复制"));
    expect(button.querySelector('[aria-live="polite"]')?.textContent).toBe(t("已复制"));
    expect(details.open).toBe(true);
    expect(toggle).not.toHaveBeenCalled();
    expect(remove).not.toHaveBeenCalled();
    expect(exportPackage).not.toHaveBeenCalled();
    expect(props.onChanged).not.toHaveBeenCalled();
    expect(props.onUpdate).not.toHaveBeenCalled();
  });

  it("keeps contributions without an invocation as text", async () => {
    const { view, props, row } = await setup();
    view.rerender(<Packages {...props} packages={[{ ...pkg,
      skills: [{ name: "implicit", description: "Model-only skill" }],
      commands: [{ name: "legacy", invocation: "" }], agents: [], prompts: [],
    }]} />);
    expect(row.getByText("implicit")).toBeTruthy();
    expect(row.getByText("legacy")).toBeTruthy();
    expect(row.getByText("paper")).toBeTruthy();
    expect(row.queryAllByRole("button", { name: /复制调用命令/ })).toHaveLength(0);
  });

  it("copies the supplied invocation exactly instead of deriving it from the display name", async () => {
    const { view, props, row, user, copy } = await setup();
    const command = "/canonical-owner:review";
    view.rerender(<Packages {...props} packages={[{ ...pkg,
      skills: [{ name: "Display title", invocation: command }],
    }]} />);
    await user.click(row.getByRole("button", { name: label(command) }));
    expect(copy).toHaveBeenCalledExactlyOnceWith(command);
  });

  it.each(["zh", "en"])("allows a denied copy to be retried in %s without closing the package", async (lang) => {
    const { row, copy, user, details } = await setup(lang);
    copy.mockRejectedValueOnce(new Error("clipboard denied"));
    const button = row.getByRole("button", { name: label(entries[0][1]) });
    await user.click(button);
    expect(button.getAttribute("title")).toBe(t("复制失败，请重试"));
    expect(button.querySelector('[aria-live="polite"]')?.textContent).toBe(t("复制失败，请重试"));
    expect(row.getByRole<HTMLButtonElement>("switch", { name: t("关闭") + " review-kit" }).disabled).toBe(false);
    await user.click(button);
    expect(button.getAttribute("title")).toBe(t("已复制"));
    expect(copy).toHaveBeenCalledTimes(2);
    expect(details.open).toBe(true);
  });

  it.each(["success", "failure"])("isolates a pending copy %s when an invocation changes", async (outcome) => {
    const { row, copy, user, view, props } = await setup();
    let finish!: () => void;
    let fail!: (error: Error) => void;
    copy.mockImplementationOnce(() => new Promise<void>((resolve, reject) => { finish = resolve; fail = reject; }));
    await user.click(row.getByRole("button", { name: label(entries[0][1]) }));
    const command = "/review-kit:current";
    view.rerender(<Packages {...props} packages={[{ ...pkg,
      skills: [{ name: "current", invocation: command }],
    }]} />);
    const current = row.getByRole("button", { name: label(command) });
    await act(async () => outcome === "success" ? finish() : fail(new Error("previous clipboard denied")));
    expect(current.getAttribute("data-state")).toBe("idle");
    expect(current.getAttribute("title")).toBe(label(command));
    await user.click(current);
    expect(copy).toHaveBeenLastCalledWith(command);
    expect(current.getAttribute("title")).toBe(t("已复制"));
  });

  it("keeps copy feedback local while package management remains available", async () => {
    const { row, copy, user, props, port } = await setup();
    const toggle = vi.spyOn(port, "setPluginEnabled").mockResolvedValue({});
    await user.click(row.getByRole("button", { name: label(entries[0][1]) }));
    expect(row.getByRole("button", { name: label(entries[1][1]) }).getAttribute("data-state")).toBe("idle");
    await user.click(row.getByRole("button", { name: "移除 review-kit" }));
    expect(row.getByRole("button", { name: "删除" })).toBeTruthy();
    await user.click(row.getByRole("button", { name: "取消" }));
    await user.click(row.getByRole("switch", { name: "关闭 review-kit" }));
    expect(toggle).toHaveBeenCalledExactlyOnceWith(pkg.name, false);
    expect(copy).toHaveBeenCalledTimes(1);
    expect(props.onChanged).toHaveBeenCalledTimes(1);
  });

  it.each(["success", "failure"])("isolates a pending copy %s after switching connections and back", async (outcome) => {
    const { row, copy, user, view, props, port } = await setup();
    let finish!: () => void;
    let fail!: (error: Error) => void;
    copy.mockImplementationOnce(() => new Promise<void>((resolve, reject) => { finish = resolve; fail = reject; }));
    await user.click(row.getByRole("button", { name: label(entries[0][1]) }));
    const next = new MockPort() as unknown as AgentPort;
    view.rerender(<Packages {...props} port={next} packages={[pkg]} />);
    view.rerender(<Packages {...props} port={port} packages={[pkg]} />);
    const details = view.container.querySelector<HTMLDetailsElement>("details")!;
    await user.click(details.querySelector("summary")!);
    const current = within(details).getByRole("button", { name: label(entries[0][1]) });
    await act(async () => outcome === "success" ? finish() : fail(new Error("previous clipboard denied")));
    expect(current.getAttribute("data-state")).toBe("idle");
    expect(props.onChanged).not.toHaveBeenCalled();
    await user.click(current);
    expect(copy).toHaveBeenCalledTimes(2);
    expect(current.getAttribute("title")).toBe(t("已复制"));
  });
});
