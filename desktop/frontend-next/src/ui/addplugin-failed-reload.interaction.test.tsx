// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { AddPlugin } from "./AddPlugin";
import { MockPort } from "../port/mock";
import { boot, STORAGE, t } from "../i18n";
import type { AgentPort, PluginPlan } from "../port/port";

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  localStorage.setItem(STORAGE, "zh");
  boot();
});

const source = "https://github.com/demo/demo";
const error = "plugin package already exists at /managed/demo; retry with replace=true to update it";
const next = "Choose another name, remove the existing entry, or retry MCP installs with replace=true.";
const reloadError = "cannot reload extensions while active work or background jobs are running";
const summary = "No action succeeded. Fix the first failed action[] entry and retry install_source with apply=true.";
const reloadWarning = "运行时未重载：{reason}。请用「重载运行时」重试。";

it.each(["zh", "en"].flatMap((lang) => [false, true].map((reload) => ({ lang, reload }))))(
  "keeps the failed action and recovery alongside the reload result ($lang, reload=$reload)",
  async ({ lang, reload }) => {
    localStorage.setItem(STORAGE, lang);
    boot();
    const port = new MockPort() as unknown as AgentPort;
    const planned: PluginPlan = {
      ok: true, status: "planned", applied: false, source, planId: "approved-demo",
      actions: [{ kind: "plugin", name: "demo", action: "install_plugin_package", status: "planned", riskLevel: "low" }],
    };
    vi.spyOn(port, "planPlugin").mockResolvedValue(planned);
    const install = vi.spyOn(port, "installPlugin").mockResolvedValue({
      ...planned, ok: false, status: "failed", applied: true, next: summary,
      actions: planned.actions!.map((action) => ({ ...action, status: "failed", error, next })),
      reloadError: reload ? reloadError : undefined,
    });
    const onInstalled = vi.fn();
    const onClose = vi.fn();
    render(<AddPlugin port={port} source={source} onInstalled={onInstalled} onClose={onClose} />);
    await userEvent.click(screen.getByRole("button", { name: t("查看内容") }));
    await userEvent.click(screen.getByRole("button", { name: t("安装") }));

    expect(document.querySelector(".outcome")?.getAttribute("data-state")).toBe("issue");
    expect(screen.getByText(t("有项目未安装成功，原因见下方。"))).toBeTruthy();
    expect(screen.queryByText(summary)).toBeNull();
    const failed = screen.getByRole("alert");
    expect(failed.textContent).toContain("demo");
    expect(failed.textContent).toContain(error);
    expect(failed.textContent).toContain(next);
    const warning = t(reloadWarning, { reason: reloadError });
    if (reload) expect(screen.getByRole("status").textContent).toBe(warning);
    else expect(screen.queryByText(warning)).toBeNull();
    expect(screen.queryByText(t("装好了，下一轮就能用"))).toBeNull();
    expect(screen.queryByText(t("装好了，但这一轮还在跑：等它结束或新建会话后生效"))).toBeNull();
    expect(onInstalled).toHaveBeenCalledOnce();
    expect(install).toHaveBeenCalledExactlyOnceWith({ source, name: undefined, replace: false, planId: planned.planId });
    await userEvent.click(screen.getByRole("button", { name: t("完成") }));
    expect(onClose).toHaveBeenCalledOnce();
    expect(install).toHaveBeenCalledOnce();
  },
);
