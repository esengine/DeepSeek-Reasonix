// @vitest-environment jsdom
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import "./testkit";
import type { TreeSession } from "../port/hub";
import { SessionMenu, type SessionMenuProps } from "./SessionMenu";

afterEach(cleanup);

function menu(over: Partial<SessionMenuProps> = {}, session: Partial<TreeSession> = {}) {
  const props: SessionMenuProps = {
    session: { path: "/p/a.jsonl", archived: false, runtimeId: "rt-1", ...session } as TreeSession,
    label: "上一次的会话", live: false, pinned: false, exporting: false, autoNaming: false,
    onPin: vi.fn(), onMarkRead: vi.fn(), onRename: vi.fn(), onAutoName: vi.fn(), onPause: vi.fn(), onClose: vi.fn(),
    onArchive: vi.fn(), onBranch: vi.fn(), onCopyInfo: vi.fn(), onExport: vi.fn(), onDelete: vi.fn(),
    onMove: vi.fn(), canMove: { up: true, down: true }, onMoveTo: vi.fn(), ...over,
  };
  render(<SessionMenu {...props} />);
  return props;
}

const groups = () => [...document.querySelectorAll(".session-pop-group")].map((g) =>
  [...g.querySelectorAll("[data-action]")].map((b) => b.getAttribute("data-action") + (b.getAttribute("data-value") ? `:${b.getAttribute("data-value")}` : "")));

describe("the session menu", () => {
  it("sorts what can be done into position, name, content, destination and ending", () => {
    menu();
    expect(groups()).toEqual([
      ["session.pin", "session.move:-1", "session.move:1"],
      ["session.rename-start", "session.auto-name", "session.mark-read"],
      ["session.branch", "session.copy-info", "session.export:md", "session.export:json"],
      ["session.move-to-start", "session.archive:archive"],
      ["session.close", "session.delete"],
    ]);
  });

  it("puts one export entry in the content group, with the two formats as choices", async () => {
    const p = menu();
    expect(screen.getAllByText("导出会话")).toHaveLength(1);
    await userEvent.click(screen.getByRole("menuitem", { name: /Markdown/ }));
    expect(p.onExport).toHaveBeenCalledWith("md");
    await userEvent.click(screen.getByRole("menuitem", { name: /JSON/ }));
    expect(p.onExport).toHaveBeenCalledWith("json");
  });

  it("opens with pause alone first while the conversation runs, and says why close and move wait", () => {
    menu({ live: true });
    expect(groups()[0]).toEqual(["session.pause"]);
    expect(screen.getByText("正在运行，暂停后才能关闭")).toBeTruthy();
    expect(screen.getByText("先关闭这个会话，再移动它")).toBeTruthy();
    expect((screen.getByRole("menuitem", { name: /关闭会话/ }) as HTMLButtonElement).disabled).toBe(true);
  });

  it("ties the reason to its item even when the path has spaces", () => {
    menu({ live: true }, { path: "C:/Users/My Name/a b.jsonl" });
    const close = screen.getByRole("menuitem", { name: "关闭会话" });
    expect(close.getAttribute("aria-describedby")).not.toMatch(/\s/);
    expect(document.getElementById(close.getAttribute("aria-describedby")!)?.textContent).toBe("正在运行，暂停后才能关闭");
  });

  it("keeps delete last and red", () => {
    menu();
    const last = [...document.querySelectorAll(".session-pop-group")].at(-1)!;
    expect(last.lastElementChild?.getAttribute("data-action")).toBe("session.delete");
    expect(last.lastElementChild?.classList.contains("danger")).toBe(true);
  });
});
