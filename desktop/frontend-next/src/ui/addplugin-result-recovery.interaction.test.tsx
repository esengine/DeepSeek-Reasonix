// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { act, cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { AddPlugin } from "./AddPlugin";
import { boot, STORAGE, t } from "../i18n";
import type { AgentPort, PluginPackage, PluginPlan } from "../port/port";

afterEach(cleanup);

const source = "/author/notes-kit";
const planned = (id: string): PluginPlan => ({
  ok: true, status: "planned", applied: false, source, planId: id,
  actions: [{ kind: "plugin", name: "notes-kit", version: "0.2.0", action: "copy_plugin", status: "planned", riskLevel: "low" }],
});
const failed: PluginPlan = {
  ...planned("consumed-plan"), ok: false, status: "failed", applied: true,
  actions: planned("consumed-plan").actions!.map((a) => ({ ...a, status: "failed", error: "The install target is unavailable", next: "Correct the target and preview again" })),
};
const installed: PluginPlan = { ...planned("fresh-plan"), status: "done", applied: true };
const updating: PluginPackage = { name: "notes-kit", source, version: "0.1.0", enabled: true, root: "/managed/notes-kit" };

function language(lang: string) {
  localStorage.setItem(STORAGE, lang);
  boot();
}

it.each(["zh", "en"])("returns a failed new installation to its source and requires a new plan (%s)", async (lang) => {
  language(lang);
  const preview = vi.fn().mockResolvedValueOnce(planned("consumed-plan")).mockResolvedValueOnce(planned("fresh-plan"));
  const install = vi.fn().mockResolvedValueOnce(failed).mockResolvedValueOnce(installed);
  const onInstalled = vi.fn();
  const onClose = vi.fn();
  const port = { planPlugin: preview, installPlugin: install } as unknown as AgentPort;
  render(<AddPlugin port={port} source={source} onClose={onClose} onInstalled={onInstalled} />);
  await userEvent.click(screen.getByRole("button", { name: t("查看内容") }));
  await userEvent.click(screen.getByRole("button", { name: t("安装") }));
  expect((await screen.findByRole("alert")).textContent).toContain("The install target is unavailable");
  const back = screen.getByRole("button", { name: t("返回") });
  await waitFor(() => expect(document.activeElement).toBe(back));
  expect(back.hasAttribute("data-primary")).toBe(true);
  await userEvent.keyboard("{Enter}");
  const input = screen.getByRole<HTMLTextAreaElement>("textbox");
  expect(input.value).toBe(source);
  await waitFor(() => expect(document.activeElement).toBe(input));
  expect(preview).toHaveBeenCalledTimes(1);
  expect(install).toHaveBeenCalledTimes(1);
  await userEvent.clear(input);
  await userEvent.type(input, "/author/corrected-kit");
  await userEvent.click(screen.getByRole("button", { name: t("查看内容") }));
  expect(preview).toHaveBeenLastCalledWith({ source: "/author/corrected-kit", name: undefined, replace: false, planId: undefined });
  expect(install).toHaveBeenCalledTimes(1);
  await userEvent.click(screen.getByRole("button", { name: t("安装") }));
  expect(install).toHaveBeenLastCalledWith({ source: "/author/corrected-kit", name: undefined, replace: false, planId: "fresh-plan" });
  expect(onInstalled).toHaveBeenCalledTimes(2);
  expect(onClose).not.toHaveBeenCalled();
  expect(screen.queryByRole("button", { name: t("返回") })).toBeNull();
});

it.each(["zh", "en"])("re-previews a failed update, recovers a preview error, and waits for confirmation (%s)", async (lang) => {
  language(lang);
  let finish!: (plan: PluginPlan) => void;
  const pending = new Promise<PluginPlan>((resolve) => { finish = resolve; });
  const preview = vi.fn().mockResolvedValueOnce(planned("consumed-plan")).mockRejectedValueOnce(new Error("source unavailable")).mockReturnValueOnce(pending);
  const install = vi.fn().mockResolvedValueOnce(failed).mockResolvedValueOnce(installed);
  const onInstalled = vi.fn();
  const onApplying = vi.fn();
  const port = { planPlugin: preview, installPlugin: install } as unknown as AgentPort;
  render(<AddPlugin port={port} updating={updating} onClose={() => {}} onInstalled={onInstalled} onApplying={onApplying} />);
  await userEvent.click(await screen.findByRole("button", { name: t("更新") }));
  expect((await screen.findByRole("alert")).textContent).toContain("The install target is unavailable");
  const retry = screen.getByRole("button", { name: t("重试") });
  await waitFor(() => expect(document.activeElement).toBe(retry));
  expect(retry.hasAttribute("data-primary")).toBe(true);
  await userEvent.keyboard("{Enter}");
  expect(await screen.findByText("source unavailable")).toBeTruthy();
  await waitFor(() => expect(document.activeElement).toBe(screen.getByRole("button", { name: t("重试") })));
  expect(install).toHaveBeenCalledTimes(1);
  await userEvent.click(screen.getByRole("button", { name: t("重试") }));
  expect(preview).toHaveBeenCalledTimes(3);
  expect(preview).toHaveBeenLastCalledWith({ source, name: "notes-kit", replace: true, planId: undefined });
  expect(screen.queryByRole("button", { name: t("更新") })).toBeNull();
  expect(screen.queryByRole("textbox")).toBeNull();
  expect(onApplying.mock.calls.map(([value]) => value)).toEqual([true, false]);
  await act(async () => finish(planned("fresh-plan")));
  const confirm = screen.getByRole("button", { name: t("更新") });
  await waitFor(() => expect(document.activeElement).toBe(confirm));
  expect(install).toHaveBeenCalledTimes(1);
  await userEvent.keyboard("{Enter}");
  expect(install).toHaveBeenLastCalledWith({ source, name: "notes-kit", replace: true, planId: "fresh-plan" });
  expect(onApplying.mock.calls.map(([value]) => value)).toEqual([true, false, true, false]);
  expect(onInstalled).toHaveBeenCalledTimes(2);
  expect(screen.queryByRole("button", { name: t("重试") })).toBeNull();
});

it.each(["zh", "en"])("allows retrying an update's initial preview without applying it (%s)", async (lang) => {
  language(lang);
  let reject!: (error: Error) => void;
  const pending = new Promise<PluginPlan>((_, fail) => { reject = fail; });
  const preview = vi.fn().mockRejectedValueOnce(new Error("source unavailable")).mockReturnValueOnce(pending).mockResolvedValueOnce(planned("fresh-plan"));
  const install = vi.fn();
  const port = { planPlugin: preview, installPlugin: install } as unknown as AgentPort;
  render(<AddPlugin port={port} updating={updating} onClose={() => {}} onInstalled={() => {}} />);
  expect(await screen.findByText("source unavailable")).toBeTruthy();
  const retry = screen.getByRole("button", { name: t("重试") });
  await waitFor(() => expect(document.activeElement).toBe(retry));
  expect(retry.hasAttribute("data-primary")).toBe(true);
  await userEvent.keyboard("{Enter}");
  await waitFor(() => expect(document.activeElement).toBe(screen.getByRole("button", { name: t("取消") })));
  await act(async () => reject(new Error("still unavailable")));
  expect(await screen.findByText("still unavailable")).toBeTruthy();
  await waitFor(() => expect(document.activeElement).toBe(screen.getByRole("button", { name: t("重试") })));
  await userEvent.keyboard("{Enter}");
  expect(await screen.findByRole("button", { name: t("更新") })).toBeTruthy();
  expect(preview).toHaveBeenCalledTimes(3);
  expect(install).not.toHaveBeenCalled();
});
