// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import "./testkit";
import { Settings } from "./Settings";
import type { SessionStatus } from "../port/port";
import { MockPort } from "../port/mock";
import { MockHub } from "../port/mock_hub";

afterEach(cleanup);

function draw(at?: string, port = new MockPort()) {
  render(
    <Settings
      hub={new MockHub() as never}
      port={port}
      at={at}
      status={{ preset: "balanced", toolApprovalMode: "ask" } as SessionStatus}
      theme="light" onTheme={() => {}} contrast="" onContrast={() => {}} weight="" onWeight={() => {}}
      look={{} as never} onLook={() => {}} reloadThemes={() => {}}
      onClose={vi.fn()} onChanged={() => {}} onError={() => {}}
      account={null} accountUnread="" reloadAccount={() => {}}
    />,
  );
}

const shown = (id: string) => {
  const el = document.querySelector<HTMLElement>(`[data-setting="${id}"]`);
  return !!el && getComputedStyle(el).display !== "none";
};

describe("the shared settings overview", () => {
  it("starts an editor at the top even when its page has no hidden sections", async () => {
    draw("tools");
    const button = await screen.findByRole("button", { name: /^查看与调整（\d+）$/ });
    const main = document.querySelector<HTMLElement>(".prefs-main")!;
    main.scrollTop = 400;
    fireEvent.click(button);
    await waitFor(() => expect(document.querySelector('.deep-page[data-setting="tool-list"]')).toBeTruthy());
    expect(main.scrollTop).toBe(0);
    main.scrollTop = 200;
    fireEvent.click(screen.getByRole("button", { name: "权限与安全" }));
    await waitFor(() => expect(document.querySelector(".deep-page")).toBeNull());
    expect(main.scrollTop).toBe(0);
  });

  it.each(["common", "model", "session", "tools", "ext", "display", "controls", "appearance", "storage", "network", "account"])("%s exposes its sections in one reading column without a duplicate index", async (at) => {
    draw(at);
    await waitFor(() => expect(document.querySelector(`.prefs-main[data-sec="${at}"] [data-setting]`)).toBeTruthy());
    expect(screen.queryByRole("navigation", { name: "本页包含" })).toBeNull();
    expect(document.querySelector(".prefs-col")?.hasAttribute("data-cols")).toBe(false);
    expect(document.querySelector(".prefs-pagetitle .count")).toBeNull();
    const sections = [...document.querySelectorAll(".prefs-col > .prefs-card")];
    for (const section of sections) expect(section.closest("details")).toBeNull();
  });
});

describe("the agent behaviour page", () => {
  it("lands on the persona editor instead of a hidden overview row", async () => {
    draw("session:persona");
    await waitFor(() => expect(document.querySelector('.deep-page[data-setting="agent-presets"] #set-persona')).toBeTruthy());
    const target = document.querySelector("#set-persona");
    expect(target?.closest(".deep-page")?.getAttribute("data-setting")).toBe("agent-presets");
    expect(document.querySelectorAll("#set-persona")).toHaveLength(1);
    expect(target?.hasAttribute("data-landed")).toBe(true);
  });

  it("uses one column with visible groups and still lands on the exact search result", async () => {
    draw("session");
    await waitFor(() => expect(shown("session-ref")).toBe(true));
    expect(document.querySelector(".prefs-col")?.hasAttribute("data-cols")).toBe(false);
    expect(document.querySelector('#set-session-ref')!.closest("details")).toBeNull();
    fireEvent.change(screen.getByRole("textbox", { name: "搜索设置" }), { target: { value: "session-ref" } });
    fireEvent.click(await screen.findByRole("option", { name: /引用历史会话/ }));
    await waitFor(() => expect(document.querySelector('#set-session-ref')?.hasAttribute("data-landed")).toBe(true));
  });

  it("retains an invalid draft and its error when search locates another setting on the page", async () => {
    const port = new MockPort();
    const read = vi.spyOn(port, "sessionRefSettings");
    const save = vi.spyOn(port, "saveSessionRefSettings");
    draw("session", port);
    const input = await screen.findByRole("textbox", { name: "引用的最大长度" });
    fireEvent.change(input, { target: { value: "1" } });
    fireEvent.blur(input);
    await screen.findByText(/请输入 500 到 200000/);
    expect(screen.queryByRole("navigation", { name: "本页包含" })).toBeNull();
    const search = screen.getByRole("textbox", { name: "搜索设置" });
    for (const [query, anchor, name] of [["无进展", "progress-watch", /长时间无进展/], ["session-ref", "session-ref", /引用历史会话/]] as const) {
      fireEvent.change(search, { target: { value: query } });
      const hit = await screen.findByRole("option", { name });
      expect(hit.getAttribute("data-target")).toBe(anchor);
      fireEvent.click(hit);
      await waitFor(() => expect(document.getElementById(`set-${anchor}`)?.hasAttribute("data-landed")).toBe(true));
    }
    expect(screen.getByRole("textbox", { name: "引用的最大长度" })).toBe(input);
    expect((input as HTMLInputElement).value).toBe("1");
    expect(screen.getByText(/请输入 500 到 200000/)).toBeTruthy();
    expect(read).toHaveBeenCalledTimes(1);
    expect(save).not.toHaveBeenCalled();
    fireEvent.change(input, { target: { value: "9000" } });
    fireEvent.blur(input);
    await waitFor(() => expect(save).toHaveBeenCalledWith(9000));
  });

  it("opens a nested editor directly and returns to the overview", async () => {
    draw("session:peers");
    await waitFor(() => expect(document.querySelector('.deep-page[data-setting="peers"]')).toBeTruthy());
    expect(screen.queryByRole("navigation", { name: "本页包含" })).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Agent 行为" }));
    await waitFor(() => expect(document.querySelector(".deep-page")).toBeNull());
    expect(document.querySelector('#set-plan-approval')).toBeTruthy();
    expect(screen.queryByRole("navigation", { name: "本页包含" })).toBeNull();
  });
});

describe("the permissions page", () => {
  it.each(["tool-list", "web-access"])("opens %s with one return path and keeps its controls usable", async (id) => {
    const port = new MockPort();
    const read = vi.spyOn(port, id === "tool-list" ? "toolSettings" : "webAccessSettings");
    draw(`tools:${id}`, port);
    await waitFor(() => expect(document.querySelector(`#set-${id}`)).toBeTruthy());
    expect(screen.getAllByRole("navigation", { name: "当前位置" })).toHaveLength(1);
    expect(document.querySelectorAll(`.deep-page[data-setting="${id}"]`)).toHaveLength(1);
    expect(screen.getAllByRole("switch").length).toBeGreaterThan(0);
    expect(read).toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "权限与安全" }));
    await waitFor(() => expect(document.querySelector(".deep-page")).toBeNull());
    expect(document.querySelector('#set-approval')).toBeTruthy();
  });

  it("is one page with sub-headings, and every block is on it", async () => {
    draw("tools");
    await waitFor(() => expect(shown("approval")).toBe(true));
    expect(shown("sandbox")).toBe(true);
    expect(shown("browser-tools")).toBe(true);
    expect([...document.querySelectorAll(".prefs-card > header h3")].map((h) => h.textContent)).toEqual(["批准", "沙箱与执行", "联网", "扩展范围"]);
    expect(document.querySelectorAll(".prefs-page")).toHaveLength(0);
  });

  it("lands on a block a deep link names", async () => {
    draw("tools:sandbox");
    await waitFor(() => expect(shown("sandbox")).toBe(true));
  });

  it("gives a short section no pages", async () => {
    draw("session");
    await waitFor(() => expect(shown("ask-timeout")).toBe(true));
    expect(document.querySelectorAll(".prefs-page")).toHaveLength(0);
  });
});

describe("the appearance family", () => {
  it("is three pages, and each setting is on one of them", async () => {
    draw("appearance");
    await waitFor(() => expect(shown("scheme")).toBe(true));
    expect(shown("keys")).toBe(false);
    expect(shown("folding")).toBe(false);
    cleanup();
    draw("controls");
    await waitFor(() => expect(shown("keys")).toBe(true));
    expect(shown("scheme")).toBe(false);
    cleanup();
    draw("display");
    await waitFor(() => expect(shown("folding")).toBe(true));
    expect(shown("keys")).toBe(false);
  });

  it("lists the shortcuts together, then what a running turn does with a message", async () => {
    draw("controls");
    await waitFor(() => expect(shown("keys")).toBe(true));
    const order = [...document.querySelectorAll<HTMLElement>("[data-setting]")]
      .filter((el) => shown(el.dataset.setting ?? ""))
      .map((el) => el.dataset.setting)
      .filter((id) => ["running-message-intent", "esc-stop", "keys"].includes(id ?? ""));
    expect(order).toEqual(["keys", "esc-stop", "running-message-intent"]);
  });

  it("lands an anchor on the page that owns it, whatever section it was opened under", async () => {
    draw("appearance:folding");
    await waitFor(() => expect(shown("folding")).toBe(true));
    expect(shown("scheme")).toBe(false);
  });
});

describe("what wraps a setting leaves with it", () => {
  it("hides the disclosure around a setting together with the setting", async () => {
    draw("appearance:navrail");
    await waitFor(() => expect(shown("navrail")).toBe(true));
    const style = [...document.querySelectorAll("style")].map((s) => s.textContent).join("\n");
    expect(style).toContain('details:has(> [data-setting="wallpaper"])');
  });
});
