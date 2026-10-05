// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { Backup } from "./Backup";
import { BackupRestore } from "./BackupRestore";
import { MockPort } from "../port/mock";
import type { AgentPort, BackupEntry } from "../port/port";

afterEach(cleanup);

const entry: BackupEntry = {
  id: "b1", label: "laptop", format: 1, appVersion: "2.20.5", platform: "darwin/arm64",
  categories: ["settings", "automation"], ciphertextBytes: 2048, createdAt: "2026-09-27T00:00:00Z",
};

describe("cloud backup", () => {
  it("starts with API keys unticked and sends the ticked categories with the passphrase", async () => {
    const port = new MockPort() as unknown as AgentPort;
    const create = vi.spyOn(port, "createBackup");
    render(<Backup port={port} />);

    const secrets = await screen.findByRole("checkbox", { name: /API 密钥/ });
    expect((secrets as HTMLInputElement).checked).toBe(false);
    const [pass, again] = document.querySelectorAll<HTMLInputElement>('input[type="password"]');
    await userEvent.type(pass!, "a long passphrase");
    await userEvent.type(again!, "a long passphrase");
    await userEvent.click(screen.getByRole("button", { name: "备份到账号" }));

    await waitFor(() => expect(create).toHaveBeenCalled());
    const req = create.mock.calls[0]![0];
    expect(req.categories).not.toContain("secrets");
    expect(req.categories).toContain("automation");
    expect(req.passphrase).toBe("a long passphrase");
  });

  it("says why the button is held back while the passphrase is short or unconfirmed", async () => {
    const port = new MockPort() as unknown as AgentPort;
    render(<Backup port={port} />);
    const btn = await screen.findByRole<HTMLButtonElement>("button", { name: "备份到账号" });
    const [pass, again] = document.querySelectorAll<HTMLInputElement>('input[type="password"]');
    expect(btn.disabled).toBe(true);
    expect(document.querySelector('[data-action="backup.wait"]')!.textContent).toBe("口令还差 10 个字符");
    await userEvent.type(pass!, "abcd");
    const why = document.querySelector('[data-action="backup.wait"]')!;
    expect(why.textContent).toBe("口令还差 6 个字符");
    expect(btn.getAttribute("aria-describedby")).toBe(why.id);
    await userEvent.type(pass!, "efghij");
    expect(document.querySelector('[data-action="backup.wait"]')!.textContent).toBe("请再输一次口令");
    await userEvent.type(again!, "abcdefghij");
    expect(document.querySelector('[data-action="backup.wait"]')).toBeNull();
    expect(btn.disabled).toBe(false);
  });

  it("asks for a category when every box is unticked", async () => {
    const port = new MockPort() as unknown as AgentPort;
    render(<Backup port={port} />);
    await screen.findByRole("button", { name: "备份到账号" });
    for (const box of screen.getAllByRole<HTMLInputElement>("checkbox")) if (box.checked) await userEvent.click(box);
    expect(document.querySelector('[data-action="backup.wait"]')!.textContent).toBe("至少选择一项备份内容");
  });

  it("holds a command back until its own consent box is ticked, and sends that consent", async () => {
    const port = new MockPort() as unknown as AgentPort;
    const apply = vi.spyOn(port, "applyBackup");
    render(<BackupRestore port={port} backup={entry} onClose={() => {}} />);

    await userEvent.type(document.querySelector<HTMLInputElement>('input[type="password"]')!, "a long passphrase");
    await userEvent.click(screen.getByRole("button", { name: "预览差异" }));
    const hookRow = await screen.findByText("PreToolUse#1");
    expect(screen.getByText("npx prettier --check .")).toBeTruthy();

    const take = hookRow.closest("li")!.querySelector<HTMLInputElement>('[data-action="backup.pick"]')!;
    expect(take.checked).toBe(false);
    await userEvent.click(take);
    const applyBtn = screen.getByRole<HTMLButtonElement>("button", { name: /恢复所选/ });
    expect(applyBtn.disabled).toBe(true);

    await userEvent.click(screen.getByRole("checkbox", { name: /允许它在本机运行/ }));
    expect(applyBtn.disabled).toBe(false);
    await userEvent.click(applyBtn);
    await waitFor(() => expect(apply).toHaveBeenCalledWith("demo", ["hook:PreToolUse#1", "memory:docs/REASONIX.md"], ["hook:PreToolUse#1"]));
  });
});
