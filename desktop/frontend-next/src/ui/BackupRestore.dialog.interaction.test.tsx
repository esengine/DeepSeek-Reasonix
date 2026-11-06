// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { BackupRestore } from "./BackupRestore";
import { MockPort } from "../port/mock";
import type { AgentPort, BackupPlan } from "../port/port";

afterEach(cleanup);

const plan = {
  planId: "p1", platform: "windows/amd64", samePlatform: true, omitted: [],
  items: [{ id: "i1", category: "settings", name: "general", status: "changed", summary: "default_model = x" }],
} as unknown as BackupPlan;

function draw(over: Partial<{ preview: () => Promise<BackupPlan>; apply: () => Promise<unknown> }> = {}) {
  const onClose = vi.fn();
  const source = {
    name: "公司笔记本",
    preview: over.preview ?? (async () => plan),
    apply: over.apply ?? (async () => ({ applied: ["i1"] })),
  };
  render(<BackupRestore port={new MockPort() as unknown as AgentPort} source={source as never} onClose={onClose} />);
  return onClose;
}

describe("restoring a backup", () => {
  it("asks for the passphrase in a dialog with Cancel and a lock on Preview differences", async () => {
    draw();
    const dialog = await screen.findByRole("dialog", { name: "恢复备份" });
    expect(dialog.textContent).toContain("输入备份「公司笔记本」的加密口令，先预览它和本机的差异，不会立即写入。");
    const foot = [...dialog.querySelectorAll("footer button")].map((b) => b.textContent);
    expect(foot).toEqual(["取消", "预览差异"]);
    expect(dialog.querySelector("footer .studio-icon")).toBeTruthy();
    expect((screen.getByRole("button", { name: "预览差异" }) as HTMLButtonElement).disabled).toBe(true);
  });

  it("closes from the cross, Cancel and Escape without previewing anything", async () => {
    const preview = vi.fn(async () => plan);
    const onClose = draw({ preview });
    await userEvent.click(await screen.findByRole("button", { name: "关闭" }));
    await userEvent.click(await screen.findByRole("button", { name: "取消" }));
    await userEvent.keyboard("{Escape}");
    expect(onClose).toHaveBeenCalledTimes(3);
    expect(preview).not.toHaveBeenCalled();
  });

  it("reads the differences in the same dialog, wider, and offers the restore as the main action", async () => {
    draw();
    await userEvent.type(await screen.findByLabelText(/口令/), "secret-pass");
    await userEvent.click(screen.getByRole("button", { name: "预览差异" }));
    const dialog = await screen.findByRole("dialog", { name: "恢复备份" });
    await waitFor(() => expect(dialog.hasAttribute("data-wide") || dialog.querySelector(".bk-plan")).toBeTruthy());
    expect(dialog.textContent).toContain("general");
    expect(screen.getByRole("button", { name: /恢复所选/ }).hasAttribute("data-primary")).toBe(true);
  });

  it("says what came of the restore in one strip per outcome, in the page", async () => {
    draw();
    await userEvent.type(await screen.findByLabelText(/口令/), "secret-pass");
    await userEvent.click(screen.getByRole("button", { name: "预览差异" }));
    await userEvent.click(await screen.findByRole("checkbox"));
    await userEvent.click(screen.getByRole("button", { name: /恢复所选/ }));
    await waitFor(() => expect(document.querySelector(".bk-result")).toBeTruthy());
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(document.querySelector('.bk-strip[data-tone="ok"]')?.textContent).toContain("已恢复 1 项。");
  });
});
