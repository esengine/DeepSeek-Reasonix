// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { BackupRestore } from "./BackupRestore";
import { MockPort } from "../port/mock";
import type { AgentPort, BackupEntry, PluginPlan } from "../port/port";

afterEach(cleanup);

const backup: BackupEntry = {
  id: "plugins", label: "laptop", format: 1, appVersion: "2.20.5", platform: "darwin/arm64",
  categories: ["extensions"], ciphertextBytes: 2048, createdAt: "2026-09-27T00:00:00Z",
};
const sources = { alpha: "/packages/alpha", beta: "/packages/beta" };

function plan(name: keyof typeof sources): PluginPlan {
  return {
    ok: true, status: "planned", applied: false, planId: `ticket-${name}`, actions: [{
      kind: "plugin", action: "install_plugin_package", name, status: "planned", riskLevel: "low", skillCount: 1,
    }],
  };
}

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (error: Error) => void;
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
}

async function setup() {
  const port = new MockPort() as unknown as AgentPort;
  vi.spyOn(port, "applyBackup").mockResolvedValue({
    applied: ["plugins"], plugins: Object.entries(sources).map(([name, source]) => ({ name, source })),
  });
  const preview = vi.spyOn(port, "planPlugin").mockImplementation(async ({ source }) => plan(source === sources.alpha ? "alpha" : "beta"));
  const install = vi.spyOn(port, "installPlugin").mockResolvedValue({ ...plan("alpha"), status: "done", applied: true, actions: [] });
  render(<BackupRestore port={port} backup={backup} onClose={() => {}} />);
  await userEvent.type(screen.getByLabelText("加密口令"), "local passphrase");
  await userEvent.click(screen.getByRole("button", { name: "预览差异" }));
  await userEvent.click(await screen.findByRole("button", { name: /恢复所选/ }));
  await screen.findByText("以下插件需要走正常的安装确认：");
  return { port, preview, install };
}

async function select(name: keyof typeof sources) {
  await userEvent.click(document.querySelector<HTMLButtonElement>(`[data-action="backup.install-plugin"][data-target="${name}"]`)!);
}

describe("restored plugin installation ownership", () => {
  it("switches the source before inspection and disables the already selected row", async () => {
    const { preview, install } = await setup();
    await select("alpha");
    expect(screen.getByRole<HTMLTextAreaElement>("textbox").value).toBe(sources.alpha);
    expect(document.querySelector<HTMLButtonElement>('[data-action="backup.install-plugin"][data-target="alpha"]')!.disabled).toBe(true);
    await select("beta");
    expect(screen.getByRole<HTMLTextAreaElement>("textbox").value).toBe(sources.beta);
    await userEvent.click(screen.getByRole("button", { name: "查看内容" }));
    await screen.findByRole("button", { name: "安装" });
    expect(preview).toHaveBeenCalledOnce();
    expect(preview).toHaveBeenCalledWith(expect.objectContaining({ source: sources.beta }));
    expect(install).not.toHaveBeenCalled();
  });

  it("discards the previous confirmation and applies only the newly inspected ticket", async () => {
    const { preview, install } = await setup();
    await select("alpha");
    await userEvent.click(screen.getByRole("button", { name: "查看内容" }));
    await screen.findByRole("button", { name: "安装" });
    await select("beta");
    expect(screen.queryByRole("button", { name: "安装" })).toBeNull();
    expect(screen.getByRole<HTMLTextAreaElement>("textbox").value).toBe(sources.beta);
    await userEvent.click(screen.getByRole("button", { name: "查看内容" }));
    await userEvent.click(await screen.findByRole("button", { name: "安装" }));
    await waitFor(() => expect(screen.queryByRole("textbox")).toBeNull());
    expect(preview).toHaveBeenCalledTimes(2);
    expect(install).toHaveBeenCalledOnce();
    expect(install).toHaveBeenCalledWith(expect.objectContaining({ source: sources.beta, planId: "ticket-beta" }));
  });

  it.each(["success", "failure"])("ignores an old pending preview's %s after switching", async (outcome) => {
    const { preview, install } = await setup();
    const pending = deferred<PluginPlan>();
    preview.mockImplementationOnce(() => pending.promise);
    await select("alpha");
    await userEvent.click(screen.getByRole("button", { name: "查看内容" }));
    await select("beta");
    expect(screen.getByRole<HTMLTextAreaElement>("textbox").value).toBe(sources.beta);
    await act(async () => {
      if (outcome === "success") pending.resolve(plan("alpha"));
      else pending.reject(new Error("old alpha preview failed"));
      await pending.promise.catch(() => {});
    });
    expect(screen.queryByText("old alpha preview failed")).toBeNull();
    expect(screen.queryByRole("button", { name: "安装" })).toBeNull();
    expect(screen.getByRole<HTMLButtonElement>("button", { name: "查看内容" }).disabled).toBe(false);
    await userEvent.click(screen.getByRole("button", { name: "查看内容" }));
    await userEvent.click(await screen.findByRole("button", { name: "安装" }));
    expect(install).toHaveBeenCalledWith(expect.objectContaining({ source: sources.beta, planId: "ticket-beta" }));
  });

  it.each(["success", "failure", "refusal"])("keeps the new confirmation when an old install returns %s", async (outcome) => {
    const { install } = await setup();
    const pending = deferred<PluginPlan>();
    install.mockImplementationOnce(() => pending.promise);
    await select("alpha");
    await userEvent.click(screen.getByRole("button", { name: "查看内容" }));
    await userEvent.click(await screen.findByRole("button", { name: "安装" }));
    await select("beta");
    await userEvent.click(screen.getByRole("button", { name: "查看内容" }));
    await screen.findByRole("button", { name: "安装" });
    await act(async () => {
      if (outcome === "failure") pending.reject(new Error("old alpha install failed"));
      else pending.resolve({ ...plan("alpha"), ok: outcome === "success", status: outcome === "success" ? "done" : "blocked", applied: outcome === "success", actions: [] });
      await pending.promise.catch(() => {});
    });
    expect(screen.queryByText("old alpha install failed")).toBeNull();
    expect(screen.getByRole<HTMLButtonElement>("button", { name: "安装" }).disabled).toBe(false);
    await userEvent.click(screen.getByRole("button", { name: "安装" }));
    expect(install).toHaveBeenNthCalledWith(1, expect.objectContaining({ source: sources.alpha, planId: "ticket-alpha" }));
    expect(install).toHaveBeenNthCalledWith(2, expect.objectContaining({ source: sources.beta, planId: "ticket-beta" }));
  });

  it("does not dismiss a later selection of the same plugin when its first install completes", async () => {
    const { install } = await setup();
    const pending = deferred<PluginPlan>();
    install.mockImplementationOnce(() => pending.promise);
    await select("alpha");
    await userEvent.click(screen.getByRole("button", { name: "查看内容" }));
    await userEvent.click(await screen.findByRole("button", { name: "安装" }));
    await select("beta");
    await select("alpha");
    await userEvent.click(screen.getByRole("button", { name: "查看内容" }));
    await screen.findByRole("button", { name: "安装" });
    await act(async () => { pending.resolve({ ...plan("alpha"), status: "done", applied: true, actions: [] }); await pending.promise; });
    expect(screen.getByRole<HTMLButtonElement>("button", { name: "安装" }).disabled).toBe(false);
    await userEvent.click(screen.getByRole("button", { name: "安装" }));
    expect(install).toHaveBeenCalledTimes(2);
  });
});
