// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { MarketGroup } from "./Market";
import { MockPort } from "../port/mock";
import type { AgentPort, MarketPlan } from "../port/port";
import { boot, t } from "../i18n";

afterEach(() => { cleanup(); vi.restoreAllMocks(); localStorage.setItem("rx-lang", "zh"); boot(); });

const account = { signedIn: true, user: { handle: "demo", email: "demo@example.com", label: "demo" } };
const port = () => new MockPort() as unknown as AgentPort;
const views = () => within(screen.getByRole("radiogroup", { name: t("社区市场") }));
const radio = (name: string) => views().getByRole<HTMLButtonElement>("radio", { name: t(name) });
const nameInput = () => screen.getByRole("textbox", { name: new RegExp("^" + t("名称")) });

describe("market author navigation keys", () => {
  it.each(["zh", "en"])("keeps one navigation Tab stop and enters the selected view in %s", async (language) => {
    localStorage.setItem("rx-lang", language); boot();
    render(<MarketGroup port={port()} account={account} onInstalled={() => {}} onSignIn={() => {}} />);
    expect(views().getAllByRole<HTMLButtonElement>("radio").filter((button) => button.tabIndex === 0)).toEqual([radio("浏览")]);
    radio("浏览").focus();
    await userEvent.tab();
    expect(document.activeElement).toBe(screen.getByRole("searchbox"));
    await userEvent.tab({ shift: true });
    expect(document.activeElement).toBe(radio("浏览"));
    await userEvent.keyboard("{ArrowLeft}");
    expect(views().getAllByRole<HTMLButtonElement>("radio").filter((button) => button.tabIndex === 0)).toEqual([radio("发布")]);
    await userEvent.tab();
    expect(document.activeElement).toBe(screen.getByRole("radio", { name: t("技能") }));
    await userEvent.tab({ shift: true });
    expect(document.activeElement).toBe(radio("发布"));
  });

  it("preserves pointer and Space activation", async () => {
    render(<MarketGroup port={port()} account={account} onInstalled={() => {}} onSignIn={() => {}} />);
    await userEvent.click(radio("我的发布"));
    expect(radio("我的发布").getAttribute("aria-checked")).toBe("true");
    radio("发布").focus();
    await userEvent.keyboard(" ");
    expect(radio("发布").getAttribute("aria-checked")).toBe("true");
    expect(nameInput()).toBeTruthy();
  });

  it("keeps applying navigation disabled and restores arrows after the own-install reply", async () => {
    const backend = port();
    const plan = await backend.planOwnMarket({ slug: "demo/ship-notes" });
    let finish!: (result: MarketPlan) => void;
    const install = vi.spyOn(backend, "installOwnMarket").mockImplementation(() => new Promise((resolve) => { finish = resolve; }));
    const changed = vi.fn();
    render(<MarketGroup port={backend} account={account} onInstalled={() => {}} onSignIn={() => {}} onApplying={changed} />);
    await userEvent.click(radio("我的发布"));
    const row = (await screen.findByText("ship-notes")).closest("li")!;
    await userEvent.click(within(row).getByRole("button", { name: "安装" }));
    await userEvent.click(await screen.findByRole("button", { name: "安装" }));
    expect(changed).toHaveBeenLastCalledWith(true);
    expect(views().getAllByRole<HTMLButtonElement>("radio").every((button) => button.disabled)).toBe(true);
    expect(fireEvent.keyDown(radio("我的发布"), { key: "ArrowRight" })).toBe(true);
    expect(radio("我的发布").getAttribute("aria-checked")).toBe("true");
    expect(install).toHaveBeenCalledTimes(1);
    await act(async () => finish({ ...plan, applied: true, status: "done" }));
    expect(changed).toHaveBeenLastCalledWith(false);
    radio("我的发布").focus();
    await userEvent.keyboard("{ArrowRight}");
    expect(document.activeElement).toBe(radio("发布"));
    expect(nameInput()).toBeTruthy();
  });

  it("reaches the author form from browse and submits its exact values using only keys", async () => {
    const backend = port();
    const mine = vi.spyOn(backend, "myMarket");
    const publish = vi.spyOn(backend, "publishMarket").mockRejectedValue(new Error("controlled publish refusal"));
    const install = vi.spyOn(backend, "installMarket");
    render(<MarketGroup port={backend} account={account} onInstalled={() => {}} onSignIn={() => {}} />);
    radio("浏览").focus();
    await userEvent.keyboard("{ArrowRight}");
    await screen.findByText("ship-notes");
    expect(mine).toHaveBeenCalledTimes(1);
    await userEvent.keyboard("{ArrowRight}");
    for (let i = 0; i < 5 && document.activeElement !== nameInput(); i++) await userEvent.tab();
    expect(document.activeElement).toBe(nameInput());
    await userEvent.keyboard("key-kit"); await userEvent.tab(); await userEvent.keyboard("1.2.3");
    await userEvent.tab(); await userEvent.keyboard("https://example.com/SKILL.md");
    for (let i = 0; i < 6; i++) await userEvent.tab();
    expect(document.activeElement).toBe(screen.getByRole("button", { name: "提交审核" }));
    await userEvent.keyboard("{Enter}");
    await screen.findByText("controlled publish refusal");
    expect(publish).toHaveBeenCalledTimes(1);
    expect(publish).toHaveBeenCalledWith({ kind: "skill", name: "key-kit", source: "https://example.com/SKILL.md", summary: "", description: "", repoUrl: "", version: "1.2.3", tags: [], visibility: "public" });
    expect(install).not.toHaveBeenCalled();
  });

  it("shows the existing sign-in action without author radio controls when signed out", () => {
    render(<MarketGroup port={port()} account={{ signedIn: false }} onInstalled={() => {}} onSignIn={() => {}} />);
    expect(screen.queryByRole("radiogroup", { name: "社区市场" })).toBeNull();
    expect(screen.getByRole("button", { name: "去登录" })).toBeTruthy();
  });
});
