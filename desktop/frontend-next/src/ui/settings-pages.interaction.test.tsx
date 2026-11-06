// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, waitFor } from "@testing-library/react";
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

describe("the permissions page", () => {
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
