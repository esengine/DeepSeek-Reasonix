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
    onClose={() => {}} onChanged={() => {}} onError={() => {}} at="model:model"
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
  await screen.findByRole("combobox", { name: t("common.default_model") });
  return { port, models, publish: (next: ModelEntry[]) => { catalog = [...base, ...next]; } };
}

async function extensions() {
  await userEvent.click(document.querySelector<HTMLButtonElement>('[data-action="settings.section"][data-value="ext"]')!);
  await screen.findByText("echo-provider");
}

async function choices() {
  await userEvent.click(document.querySelector<HTMLButtonElement>('[data-action="settings.section"][data-value="model"]')!);
  await userEvent.click(await waitFor(() => document.querySelector<HTMLButtonElement>('[data-action="settings.open-page"][data-target="model"]')!));
  return screen.findByRole<HTMLSelectElement>("combobox", { name: t("common.default_model") });
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
  await userEvent.click(document.querySelector<HTMLButtonElement>('[data-action="extensions.add"][data-value="source"]')!);
  await userEvent.type(document.querySelector<HTMLTextAreaElement>(".addpkg textarea")!, "https://example.invalid/echo-provider");
  await userEvent.click(screen.getByRole("button", { name: t("ext.see_what_it_is") }));
  await userEvent.click(await screen.findByRole("button", { name: t("ext.install") }));
  await screen.findByRole("button", { name: t("common.done") });
  const select = await choices();
  await waitFor(() => expect(refs(select)).toContain(echo.ref));
  expect(refs(screen.getByRole<HTMLSelectElement>("combobox", { name: t("common.subagents") }))).toContain(echo.ref);
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
    await userEvent.click(row.getByRole("button", { name: t("common.remove_name", { name: "echo-provider" }) }));
    await userEvent.click(row.getByRole("button", { name: t("common.delete") }));
  } else if (operation === "update") {
    await userEvent.click(row.getByRole("button", { name: t("ext.update") }));
    const panel = await waitFor(() => {
      const found = document.querySelector<HTMLElement>('.addpkg[data-stage="confirm"]');
      expect(found).toBeTruthy();
      return found!;
    });
    await userEvent.click(within(panel).getByRole("button", { name: t("ext.update") }));
    await screen.findByRole("button", { name: t("common.done") });
  } else {
    await userEvent.click(row.getByRole("switch", { name: t(operation === "disable" ? "common.close" : "ext.enable") + " echo-provider" }));
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
  await userEvent.click(screen.getByRole("button", { name: t("app.reload_runtime") }));
  await screen.findByText(t("app.live_the_next_turn_runs"));
  const select = await choices();
  await waitFor(() => expect(refs(select)).toContain(echo.ref));
});

it("keeps the old live catalog after a refused reload and refreshes only after retry succeeds", async () => {
  const { port, models, publish } = await fixture(false);
  vi.spyOn(port, "reloadExtensions").mockRejectedValueOnce(new Error("reload refused")).mockImplementation(async () => { publish([echo]); });
  await extensions();
  const before = models.mock.calls.length;
  await userEvent.click(screen.getByRole("button", { name: t("app.reload_runtime") }));
  await screen.findByText("reload refused");
  expect(models).toHaveBeenCalledTimes(before);
  expect(refs(await choices())).not.toContain(echo.ref);
  await extensions();
  await userEvent.click(screen.getByRole("button", { name: t("app.reload_runtime") }));
  await screen.findByText(t("app.live_the_next_turn_runs"));
  const select = await choices();
  await waitFor(() => expect(refs(select)).toContain(echo.ref));
});

it("shows the existing unavailable state when the refreshed catalog fails", async () => {
  const { port, models } = await fixture();
  vi.spyOn(port, "reloadExtensions").mockResolvedValue();
  await extensions();
  models.mockRejectedValue(new Error("model catalog unavailable"));
  await userEvent.click(screen.getByRole("button", { name: t("app.reload_runtime") }));
  await screen.findByText(t("app.live_the_next_turn_runs"));
  await userEvent.click(document.querySelector<HTMLButtonElement>('[data-action="settings.section"][data-value="model"]')!);
  await userEvent.click(await waitFor(() => document.querySelector<HTMLButtonElement>('[data-action="settings.open-page"][data-target="model"]')!));
  expect(await screen.findByText(t("usage.could_not_read_the_model"))).toBeTruthy();
  expect(screen.queryByRole("combobox", { name: t("common.default_model") })).toBeNull();
});

it("does not refresh models when installation writes nothing", async () => {
  const { port, models } = await fixture(false);
  vi.spyOn(port, "installPlugin").mockResolvedValue({ ok: false, applied: false, status: "denied", error: "install refused" });
  await extensions();
  const before = models.mock.calls.length;
  await userEvent.click(document.querySelector<HTMLButtonElement>('[data-action="extensions.add"]')!);
  await userEvent.click(document.querySelector<HTMLButtonElement>('[data-action="extensions.add"][data-value="source"]')!);
  await userEvent.type(document.querySelector<HTMLTextAreaElement>(".addpkg textarea")!, "https://example.invalid/echo-provider");
  await userEvent.click(screen.getByRole("button", { name: t("ext.see_what_it_is") }));
  await userEvent.click(await screen.findByRole("button", { name: t("ext.install") }));
  await screen.findByText("install refused");
  expect(models).toHaveBeenCalledTimes(before);
  expect(refs(await choices())).not.toContain(echo.ref);
});

it("leaves the live model catalog unchanged when a toggle fails", async () => {
  const { port, models } = await fixture();
  vi.spyOn(port, "setPluginEnabled").mockRejectedValue(new Error("toggle refused"));
  await extensions();
  await userEvent.click(screen.getByRole("switch", { name: t("common.close") + " echo-provider" }));
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
  await userEvent.click(document.querySelector<HTMLButtonElement>('[data-action="extensions.add"][data-value="source"]')!);
  await userEvent.type(document.querySelector<HTMLTextAreaElement>(".addpkg textarea")!, "https://example.invalid/echo-provider");
  await userEvent.click(screen.getByRole("button", { name: t("ext.see_what_it_is") }));
  await userEvent.click(await screen.findByRole("button", { name: t("ext.install") }));
  await screen.findByRole("button", { name: t("common.done") });
  expect(refs(await choices())).not.toContain(echo.ref);
  await extensions();
  await userEvent.click(screen.getByRole("button", { name: t("app.reload_runtime") }));
  await screen.findByText(t("app.live_the_next_turn_runs"));
  const select = await choices();
  await waitFor(() => expect(refs(select)).toContain(echo.ref));
});
