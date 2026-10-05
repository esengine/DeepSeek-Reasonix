// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { Settings } from "./Settings";
import type { AgentPort, SessionStatus } from "../port/port";
import { MockPort } from "../port/mock";
import { MockHub } from "../port/mock_hub";

afterEach(cleanup);

function draw() {
  const onClose = vi.fn();
  // The tree's own mock, not a hand-built stand-in: a fixture that answers
  // "nothing" in a shape the real screen never sees would be testing itself.
  const port = new MockPort();
  const hub = new MockHub();
  const calls: string[] = [];
  const watched = new Proxy(port, {
    get: (own, key: string) => {
      const v = (own as unknown as Record<string, unknown>)[key];
      if (typeof v !== "function") return v;
      return (...a: unknown[]) => {
        calls.push(key);
        return (v as (...x: unknown[]) => unknown).apply(own, a);
      };
    },
  }) as unknown as AgentPort;
  render(
    <Settings
      hub={hub as never}
      port={watched}
      status={{ preset: "balanced", toolApprovalMode: "ask" } as SessionStatus}
      theme="light" onTheme={() => {}} contrast="" onContrast={() => {}} weight="" onWeight={() => {}}
      look={{} as never} onLook={() => {}} reloadThemes={() => {}}
      onClose={onClose} onChanged={() => {}} onError={() => {}}
      account={null} accountUnread="" reloadAccount={() => {}}
    />,
  );
  const find = () => screen.getByRole("textbox", { name: "搜索设置" });
  const block = (id: string) => document.querySelector(`[data-setting="${id}"]`);
  return { calls, onClose, find, block };
}

describe("finding a setting", () => {
  // The corpus is the table, not the screen. Reading the DOM could only ever
  // search the page already open, and mounting the others to search them is
  // how a hidden page starts making requests nobody asked for.
  it("finds a setting on a page that is not open", async () => {
    const { find, block } = draw();
    expect(block("network")).toBeNull();
    await userEvent.type(find(), "代理");
    const hit = await screen.findByRole("option", { name: /网络/ });
    expect(hit.getAttribute("data-value")).toBe("network");
    expect(hit.getAttribute("data-target")).toBe("network");
  });

  // The nav's own summary already reads a few of these when the sheet opens,
  // so "never called" would be a claim about the wrong thing. What searching
  // must not do is add a read, or put a page on screen to look inside it.
  it("does not open that page to find it", async () => {
    const { find, calls, block } = draw();
    await waitFor(() => expect(calls.length).toBeGreaterThan(0));
    const before = calls.length;
    await userEvent.type(find(), "代理");
    await screen.findByRole("option", { name: /网络/ });
    expect(block("network")).toBeNull();
    expect(calls.length).toBe(before);
  });

  it("goes to the block, not just the page", async () => {
    const { find, block } = draw();
    await userEvent.type(find(), "代理");
    await userEvent.click(await screen.findByRole("option", { name: /网络/ }));
    await waitFor(() => expect(block("network")).toBeTruthy());
    expect((find() as HTMLInputElement).value).toBe("");
  });

  // A block folded into a disclosure is in the DOM and not on screen, so
  // finding it is not the same as arriving at it: the jump has to open what it
  // is folded into, or it lands the reader on a blank stretch of the page.
  it("opens the disclosure the block is folded behind", async () => {
    const { find, block } = draw();
    await userEvent.type(find(), "柔和");
    await userEvent.click(await screen.findByRole("option", { name: /文字对比度/ }));
    await waitFor(() => expect(block("contrast")).toBeTruthy());
    expect(block("contrast")?.closest<HTMLDetailsElement>("details.advset")?.open).toBe(true);
  });

  // An alias is a way in. "代理" is not what this setting is called, and the
  // result says the setting's own name — otherwise the word someone guessed
  // would start standing in for what the thing is.
  it("answers an alias with the setting's own name", async () => {
    const { find } = draw();
    await userEvent.type(find(), "代理");
    const hit = await screen.findByRole("option", { name: /网络/ });
    expect(hit.textContent).toContain("网络");
    expect(hit.textContent).not.toContain("代理");
  });

  it("leaves the page alone when the query is cleared", async () => {
    const { find, block } = draw();
    await userEvent.type(find(), "代理");
    await userEvent.click(await screen.findByRole("option", { name: /网络/ }));
    await waitFor(() => expect(block("network")).toBeTruthy());
    await userEvent.type(find(), "沙箱");
    await userEvent.clear(find());
    expect(block("network")).toBeTruthy();
  });

  // One Escape, one layer.
  it("takes the search back before it takes the settings", async () => {
    const { find, onClose } = draw();
    await userEvent.type(find(), "代理");
    await userEvent.keyboard("{Escape}");
    expect((find() as HTMLInputElement).value).toBe("");
    expect(onClose).not.toHaveBeenCalled();
  });

  it("says so rather than showing nothing", async () => {
    const { find } = draw();
    await userEvent.type(find(), "zzzz");
    expect(await screen.findByText("没有匹配的设置")).toBeTruthy();
  });
});
