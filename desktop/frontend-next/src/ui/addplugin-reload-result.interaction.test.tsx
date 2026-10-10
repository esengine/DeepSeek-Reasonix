// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { AddPlugin } from "./AddPlugin";
import { MockPort } from "../port/mock";
import { boot, STORAGE, t } from "../i18n";
import type { AgentPort } from "../port/port";

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  localStorage.setItem(STORAGE, "zh");
  boot();
});

const reasons = [
  "cannot reload extensions while active work or background jobs are running",
  "reload extensions: unable to secure replacement session",
];
const warning = "装好了，但运行时未重载：{reason}。请用「重载运行时」重试。";

it.each(["zh", "en"].flatMap((lang) => [false, true].flatMap((updating) =>
  reasons.map((reloadError) => ({ lang, updating, reloadError })),
)))("preserves the host reload reason after an applied install ($lang, updating=$updating, $reloadError)", async ({ lang, updating, reloadError }) => {
  localStorage.setItem(STORAGE, lang);
  boot();
  const port = new MockPort() as unknown as AgentPort;
  const current = (await port.plugins())[0]!;
  const source = current.source!;
  const original = port.installPlugin.bind(port);
  const install = vi.spyOn(port, "installPlugin").mockImplementation(async (request) => ({
    ...await original(request), reloadError,
  }));
  const onInstalled = vi.fn();
  const onClose = vi.fn();
  render(<AddPlugin port={port} source={source} updating={updating ? current : undefined} onInstalled={onInstalled} onClose={onClose} />);
  if (!updating) await userEvent.click(screen.getByRole("button", { name: t("查看内容") }));
  await userEvent.click(await screen.findByRole("button", { name: t(updating ? "更新" : "安装") }));

  const message = screen.getByText(t(warning, { reason: reloadError }));
  expect(message.closest(".outcome")?.getAttribute("data-state")).toBe("action_required");
  expect(screen.queryByText(t("装好了，下一轮就能用"))).toBeNull();
  expect(screen.queryByText(t("装好了，但这一轮还在跑：等它结束或新建会话后生效"))).toBeNull();
  expect(onInstalled).toHaveBeenCalledTimes(1);
  expect(install).toHaveBeenCalledExactlyOnceWith(expect.objectContaining({ source, replace: updating, planId: expect.any(String) }));
  await userEvent.click(screen.getByRole("button", { name: t("完成") }));
  expect(onClose).toHaveBeenCalledOnce();
  expect(install).toHaveBeenCalledOnce();
});
