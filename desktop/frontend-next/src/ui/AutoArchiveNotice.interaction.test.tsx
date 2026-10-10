// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { AutoArchiveNotice } from "./AutoArchiveNotice";
import type { TreeWorkspace } from "../port/hub";

beforeEach(() => localStorage.clear());
afterEach(cleanup);

const row = (path: string, extra: object = {}) => ({ path, name: path, ...extra });
const tree = [{ root: "/w", name: "w", sessions: [
  row("a", { archived: true, autoArchivedAt: 1000 }),
  row("b", { archived: true, autoArchivedAt: 2000 }),
  row("c", { archived: true }),
  row("d"),
] }] as unknown as TreeWorkspace[];

describe("the automatic-archive notice", () => {
  it("counts only conversations the kernel archived, and says so once", async () => {
    const { container, rerender } = render(<AutoArchiveNotice tree={tree} />);
    expect(screen.getByRole("status").textContent).toContain("已自动归档 2 个会话，可在“已归档”中恢复。");
    await userEvent.click(screen.getByRole("button", { name: "知道了" }));
    expect(container.textContent).toBe("");
    rerender(<AutoArchiveNotice tree={tree} />);
    expect(container.textContent).toBe("");
  });

  it("speaks again for a later sweep", async () => {
    localStorage.setItem("reasonix:auto-archive-seen", "2000");
    const later = [{ ...tree[0], sessions: [...tree[0].sessions, row("e", { archived: true, autoArchivedAt: 3000 })] }] as unknown as TreeWorkspace[];
    render(<AutoArchiveNotice tree={later} />);
    expect(screen.getByRole("status").textContent).toContain("已自动归档 1 个会话");
  });
});
