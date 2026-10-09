// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { Settings } from "./Settings";
import { MockPort } from "../port/mock";
import { MockHub } from "../port/mock_hub";
import type { AgentPort, ModelEntry, SessionStatus } from "../port/port";
import { boot, STORAGE, t } from "../i18n";

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  localStorage.setItem(STORAGE, "zh");
  boot();
});

const echo: ModelEntry = {
  ref: "plugin/echo-provider/offline/echo", provider: "plugin/echo-provider/offline",
  model: "echo", kind: "extension", contextWindow: 64000,
};

function draw(port: AgentPort) {
  return <Settings
    hub={new MockHub() as never} port={port}
    status={{ preset: "balanced", toolApprovalMode: "ask" } as SessionStatus}
    theme="light" onTheme={() => {}} contrast="" onContrast={() => {}} weight="" onWeight={() => {}}
    look={{} as never} onLook={() => {}} reloadThemes={() => {}}
    onClose={() => {}} onChanged={() => {}} onError={() => {}} at="model"
    account={null} accountUnread="" reloadAccount={() => {}}
  />;
}

async function fixture(enabled = true) {
  const port = new MockPort() as unknown as AgentPort;
  const base = await port.models();
  let catalog = enabled ? [...base, echo] : base;
  const models = vi.spyOn(port, "models").mockImplementation(async () => catalog);
  const packages = await port.plugins();
  vi.spyOn(port, "plugins").mockResolvedValue([
    { ...packages[0], name: "echo-provider", enabled, runtime: { command: "bin/echo-provider.exe", capabilities: ["providers"] } },
  ]);
  render(draw(port));
  await screen.findByRole("combobox", { name: t("默认模型") });
  return { port, models, publish: (next: ModelEntry[]) => { catalog = [...base, ...next]; } };
}

async function extensions() {
  await userEvent.click(document.querySelector<HTMLButtonElement>('[data-action="settings.section"][data-value="ext"]')!);
  await screen.findByText("echo-provider");
}

async function choices() {
  await userEvent.click(document.querySelector<HTMLButtonElement>('[data-action="settings.section"][data-value="model"]')!);
  return screen.findByRole<HTMLSelectElement>("combobox", { name: t("默认模型") });
}

const refs = (select: HTMLSelectElement) => Array.from(select.options, (option) => option.value);

it.each(["zh", "en"])("refreshes a newly installed provider into the still-open model picker (%s)", async (language) => {
  localStorage.setItem(STORAGE, language);
  boot();
  const { port, publish } = await fixture(false);
  const install = port.installPlugin.bind(port);
  vi.spyOn(port, "installPlugin").mockImplementation(async (req) => {
    const out = await install(req);
    publish([echo]);
    return out;
  });
  const set = vi.spyOn(port, "setModel");
  const setDefault = vi.spyOn(port, "setDefaultModel");
  await extensions();
  await userEvent.click(document.querySelector<HTMLButtonElement>('[data-action="extensions.add"]')!);
  await userEvent.type(document.querySelector<HTMLTextAreaElement>(".addpkg textarea")!, "https://example.invalid/echo-provider");
  await userEvent.click(screen.getByRole("button", { name: t("查看内容") }));
  await userEvent.click(await screen.findByRole("button", { name: t("安装") }));
  await screen.findByRole("button", { name: t("完成") });
  const select = await choices();
  await waitFor(() => expect(refs(select)).toContain(echo.ref));
  expect(refs(screen.getByRole<HTMLSelectElement>("combobox", { name: t("子代理") }))).toContain(echo.ref);
  await userEvent.selectOptions(select, echo.ref);
  expect(set).toHaveBeenCalledWith(echo.ref);
  expect(setDefault).toHaveBeenCalledWith(echo.ref);
});

it.each(["disable", "enable", "remove", "update"])("re-reads provider models after a package %s", async (operation) => {
  const { port, publish } = await fixture(operation !== "enable");
  const next = operation === "disable" || operation === "remove" ? [] : operation === "update" ? [{ ...echo, ref: "plugin/echo-provider/offline/echo-v2", model: "echo-v2" }] : [echo];
  if (operation === "remove") vi.spyOn(port, "removePlugin").mockImplementation(async () => {
    publish(next);
    return { ok: true, applied: true, status: "done", actions: [] };
  });
  else if (operation === "update") vi.spyOn(port, "installPlugin").mockImplementation(async () => {
    publish(next);
    return { ok: true, applied: true, status: "done", actions: [] };
  });
  else vi.spyOn(port, "setPluginEnabled").mockImplementation(async () => { publish(next); return {}; });
  await extensions();
  const row = within(document.querySelector<HTMLElement>('[data-extension-name="echo-provider"]')!);
  if (operation === "remove") {
    await userEvent.click(row.getByRole("button", { name: t("移除 {name}", { name: "echo-provider" }) }));
    await userEvent.click(row.getByRole("button", { name: t("删除") }));
  } else if (operation === "update") {
    await userEvent.click(row.getByRole("button", { name: t("更新") }));
    const panel = await waitFor(() => {
      const found = document.querySelector<HTMLElement>('.addpkg[data-stage="confirm"]');
      expect(found).toBeTruthy();
      return found!;
    });
    await userEvent.click(within(panel).getByRole("button", { name: t("更新") }));
    await screen.findByRole("button", { name: t("完成") });
  } else {
    await userEvent.click(row.getByRole("switch", { name: t(operation === "disable" ? "关闭" : "启用") + " echo-provider" }));
  }
  const select = await choices();
  await waitFor(() => {
    expect(refs(select).includes(echo.ref)).toBe(operation === "enable");
    if (operation === "update") expect(refs(select)).toContain(next[0].ref);
  });
});

it("refreshes models after a manual runtime reload", async () => {
  const { port, publish } = await fixture(false);
  vi.spyOn(port, "reloadExtensions").mockImplementation(async () => { publish([echo]); });
  await extensions();
  await userEvent.click(screen.getByRole("button", { name: t("重载运行时") }));
  await screen.findByText(t("已生效，下一轮开始用新的扩展"));
  const select = await choices();
  await waitFor(() => expect(refs(select)).toContain(echo.ref));
});

it("keeps the old live catalog after a refused reload and refreshes only after retry succeeds", async () => {
  const { port, models, publish } = await fixture(false);
  vi.spyOn(port, "reloadExtensions").mockRejectedValueOnce(new Error("reload refused")).mockImplementation(async () => { publish([echo]); });
  await extensions();
  const before = models.mock.calls.length;
  await userEvent.click(screen.getByRole("button", { name: t("重载运行时") }));
  await screen.findByText("reload refused");
  expect(models).toHaveBeenCalledTimes(before);
  expect(refs(await choices())).not.toContain(echo.ref);
  await extensions();
  await userEvent.click(screen.getByRole("button", { name: t("重载运行时") }));
  await screen.findByText(t("已生效，下一轮开始用新的扩展"));
  const select = await choices();
  await waitFor(() => expect(refs(select)).toContain(echo.ref));
});

it("shows the existing unavailable state when the refreshed catalog fails", async () => {
  const { port, models } = await fixture();
  vi.spyOn(port, "reloadExtensions").mockResolvedValue();
  await extensions();
  models.mockRejectedValue(new Error("model catalog unavailable"));
  await userEvent.click(screen.getByRole("button", { name: t("重载运行时") }));
  await screen.findByText(t("已生效，下一轮开始用新的扩展"));
  await userEvent.click(document.querySelector<HTMLButtonElement>('[data-action="settings.section"][data-value="model"]')!);
  expect(await screen.findByText(t("无法读取模型列表。"))).toBeTruthy();
  expect(screen.queryByRole("combobox", { name: t("默认模型") })).toBeNull();
});

it("does not refresh models when installation writes nothing", async () => {
  const { port, models } = await fixture(false);
  vi.spyOn(port, "installPlugin").mockResolvedValue({ ok: false, applied: false, status: "denied", error: "install refused" });
  await extensions();
  const before = models.mock.calls.length;
  await userEvent.click(document.querySelector<HTMLButtonElement>('[data-action="extensions.add"]')!);
  await userEvent.type(document.querySelector<HTMLTextAreaElement>(".addpkg textarea")!, "https://example.invalid/echo-provider");
  await userEvent.click(screen.getByRole("button", { name: t("查看内容") }));
  await userEvent.click(await screen.findByRole("button", { name: t("安装") }));
  await screen.findByText("install refused");
  expect(models).toHaveBeenCalledTimes(before);
  expect(refs(await choices())).not.toContain(echo.ref);
});

it("leaves the live model catalog unchanged when a toggle fails", async () => {
  const { port, models } = await fixture();
  vi.spyOn(port, "setPluginEnabled").mockRejectedValue(new Error("toggle refused"));
  await extensions();
  await userEvent.click(screen.getByRole("switch", { name: t("关闭") + " echo-provider" }));
  await screen.findByText("toggle refused");
  expect(refs(await choices())).toContain(echo.ref);
  expect(models).toHaveBeenCalled();
});

it("reads the live catalog after a saved installation whose reload was refused, then refreshes after retry", async () => {
  const { port, publish } = await fixture(false);
  vi.spyOn(port, "installPlugin").mockResolvedValue({ ok: true, applied: true, status: "done", reloadError: "reload refused" });
  vi.spyOn(port, "reloadExtensions").mockImplementation(async () => { publish([echo]); });
  await extensions();
  await userEvent.click(document.querySelector<HTMLButtonElement>('[data-action="extensions.add"]')!);
  await userEvent.type(document.querySelector<HTMLTextAreaElement>(".addpkg textarea")!, "https://example.invalid/echo-provider");
  await userEvent.click(screen.getByRole("button", { name: t("查看内容") }));
  await userEvent.click(await screen.findByRole("button", { name: t("安装") }));
  await screen.findByRole("button", { name: t("完成") });
  expect(refs(await choices())).not.toContain(echo.ref);
  await extensions();
  await userEvent.click(screen.getByRole("button", { name: t("重载运行时") }));
  await screen.findByText(t("已生效，下一轮开始用新的扩展"));
  const select = await choices();
  await waitFor(() => expect(refs(select)).toContain(echo.ref));
});
