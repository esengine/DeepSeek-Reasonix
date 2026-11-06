// @vitest-environment jsdom
import "./testkit";
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { ScopeAway, ScopeRow } from "./CapabilityScope";
import type { CapabilityScope } from "../port/port";

afterEach(cleanup);

const scope = (over: Partial<CapabilityScope> = {}): CapabilityScope => ({
  key: "a", root: "/work/app", name: "app", label: "", repo: true, trees: 1, current: true, overrides: 0, branch: "main", ...over,
}) as CapabilityScope;
const scopes = [scope(), scope({ key: "b", root: "/work/site", name: "site", current: false, overrides: 2 })];

describe("the extension scope row in settings", () => {
  it("names the project in a pill, with where it is and what it carries below", () => {
    render(<ScopeRow scope={scope({ trees: 15 })} scopes={scopes} onPick={() => {}} />);
    expect(screen.getByRole("button", { name: /app/ })).toBeTruthy();
    expect(screen.getByText("管理哪个项目的扩展")).toBeTruthy();
    const info = document.querySelector(".scopeinfo")!;
    expect(info.textContent).toContain("/work/app");
    expect(info.textContent).toContain("main");
    expect(info.textContent).toContain("15 个工作树共用这份设置");
    expect(info.textContent).toContain("没有本项目的例外");
  });

  it("counts the exceptions a project carries", () => {
    render(<ScopeRow scope={scope({ overrides: 3 })} scopes={scopes} onPick={() => {}} />);
    expect(document.querySelector(".scopeinfo")!.textContent).toContain("3 项只在这里生效");
  });

  it("opens the project list from the pill and hands back the chosen root", async () => {
    const onPick = vi.fn();
    render(<ScopeRow scope={scope()} scopes={scopes} onPick={onPick} />);
    await userEvent.click(screen.getByRole("button", { name: /app/ }));
    await userEvent.click(screen.getByRole("option", { name: /site/ }));
    expect(onPick).toHaveBeenCalledWith("/work/site");
    expect(screen.queryByRole("option")).toBeNull();
  });
});

describe("the extension page naming another project", () => {
  it("says whose list it is and offers the way back", async () => {
    const onBack = vi.fn();
    render(<ScopeAway scope={scope({ name: "site", current: false })} onBack={onBack} />);
    expect(screen.getByRole("note").textContent).toContain("正在管理 site 的扩展，不是当前会话所在的项目。");
    await userEvent.click(screen.getByRole("button", { name: "回到当前项目" }));
    expect(onBack).toHaveBeenCalledOnce();
  });
});

describe("the project pill on the extension page", () => {
  it("opens the same list in place and hands back the chosen root", async () => {
    const { ScopePill } = await import("./CapabilityScope");
    const onPick = vi.fn();
    render(<ScopePill scope={scope()} scopes={scopes} onPick={onPick} />);
    await userEvent.click(screen.getByRole("button", { name: /app/ }));
    await userEvent.click(screen.getByRole("option", { name: /site/ }));
    expect(onPick).toHaveBeenCalledWith("/work/site");
  });
});
