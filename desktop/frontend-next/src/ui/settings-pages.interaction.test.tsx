// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { Settings } from "./Settings";
import type { SessionStatus } from "../port/port";
import { MockPort } from "../port/mock";
import { MockHub } from "../port/mock_hub";

afterEach(cleanup);

function draw(at?: string) {
  render(
    <Settings
      hub={new MockHub() as never}
      port={new MockPort()}
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

describe("pages inside the tools section", () => {
  it("opens on its first page and shows only what is filed there", async () => {
    draw("tools");
    await screen.findByRole("tab", { name: /批准与规则/ });
    expect(shown("approval")).toBe(true);
    expect(shown("sandbox")).toBe(false);
    expect(shown("web-access")).toBe(false);
  });

  it("moves to another page from the navigation and back", async () => {
    draw("tools");
    await userEvent.click(await screen.findByRole("tab", { name: /沙箱与执行/ }));
    await waitFor(() => expect(shown("sandbox")).toBe(true));
    expect(shown("approval")).toBe(false);
    await userEvent.click(screen.getByRole("tab", { name: /批准与规则/ }));
    await waitFor(() => expect(shown("approval")).toBe(true));
    expect(shown("sandbox")).toBe(false);
  });

  it("starts a newly chosen page at the top", async () => {
    draw("tools");
    await screen.findByRole("tab", { name: /沙箱与执行/ });
    screen.getByRole("tabpanel").scrollTop = 900;
    await userEvent.click(screen.getByRole("tab", { name: /沙箱与执行/ }));
    await waitFor(() => expect(shown("sandbox")).toBe(true));
    expect(screen.getByRole("tabpanel").scrollTop).toBe(0);
  });

  it("lands on the page a deep link names", async () => {
    draw("tools:sandbox");
    await waitFor(() => expect(shown("sandbox")).toBe(true));
    expect(shown("approval")).toBe(false);
    expect(screen.getByRole("tab", { name: /沙箱与执行/ }).getAttribute("aria-selected")).toBe("true");
  });

  it("lands on the page a search result is on", async () => {
    draw("tools");
    await userEvent.type(screen.getByRole("textbox", { name: "搜索设置" }), "内置浏览器");
    await userEvent.click(await screen.findByRole("option", { name: /内置浏览器/ }));
    await waitFor(() => expect(shown("browser-tools")).toBe(true));
    expect(shown("approval")).toBe(false);
  });

  it("gives a short section no pages", async () => {
    draw("session");
    await waitFor(() => expect(shown("preset")).toBe(true));
    expect(document.querySelectorAll(".prefs-page")).toHaveLength(0);
    expect(shown("preset")).toBe(true);
  });
});

describe("pages inside the appearance section", () => {
  it("splits it into pages and keeps each setting on one", async () => {
    draw("appearance");
    await screen.findByRole("tab", { name: /导航与侧栏/ });
    expect(shown("mode")).toBe(true);
    expect(shown("send-shortcut")).toBe(false);
    await userEvent.click(screen.getByRole("tab", { name: /快捷键/ }));
    await waitFor(() => expect(shown("send-shortcut")).toBe(true));
    expect(shown("mode")).toBe(false);
  });
});

describe("the shortcuts page", () => {
  it("lists what stops or sends a turn together, before the key table", async () => {
    draw("appearance");
    await userEvent.click(await screen.findByRole("tab", { name: /快捷键/ }));
    await waitFor(() => expect(shown("send-shortcut")).toBe(true));
    const order = [...document.querySelectorAll<HTMLElement>("[data-setting]")]
      .filter((el) => shown(el.dataset.setting ?? ""))
      .map((el) => el.dataset.setting);
    expect(order).toEqual(["send-shortcut", "running-message-intent", "esc-stop", "keys"]);
  });
});

describe("what wraps a setting leaves with it", () => {
  it("hides the disclosure around a setting together with the setting", async () => {
    draw("appearance:navrail");
    await waitFor(() => expect(shown("navrail")).toBe(true));
    const style = [...document.querySelectorAll("style")].map((s) => s.textContent).join("\n");
    expect(style).toContain('details:has(> [data-setting="wallpaper"])');
    expect(style).toContain('details:has(> [data-setting="advanced-appearance"])');
  });
});
