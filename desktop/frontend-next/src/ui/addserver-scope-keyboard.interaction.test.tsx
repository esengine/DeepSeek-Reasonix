// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { act, cleanup, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { boot, STORAGE, t } from "../i18n";
import { MockPort } from "../port/mock";
import type { AgentPort, McpDraft, McpInstallResult } from "../port/port";
import { AddServer } from "./AddServer";

afterEach(() => { cleanup(); localStorage.setItem(STORAGE, "zh"); boot(); });

const draft: McpDraft = { servers: [{ name: "docs", transport: "http", url: "https://example.test/mcp" }], risks: [] };
const ready: McpInstallResult = { name: "docs", state: "ready", toolCount: 1, action: "installed", message: "" };

async function setup(lang: string, canProject = true) {
  localStorage.setItem(STORAGE, lang); boot();
  const port = new MockPort() as unknown as AgentPort;
  vi.spyOn(port, "parseMcp").mockResolvedValue(draft);
  const installed = vi.fn();
  const close = vi.fn();
  render(<AddServer port={port} canProject={canProject} onClose={close} onInstalled={installed} />);
  const user = userEvent.setup();
  await user.type(screen.getByRole("textbox"), "https://example.test/mcp");
  await user.click(screen.getByRole("button", { name: t("查看内容") }));
  const radios = within(screen.getByRole("radiogroup", { name: t("安装位置") })).getAllByRole<HTMLButtonElement>("radio");
  return { port, installed, close, user, radios };
}

it.each(["zh", "en"])("moves focus and selection with arrows before installing in the selected scope (%s)", async (lang) => {
  const { port, installed, close, user, radios } = await setup(lang);
  let finish!: (value: McpInstallResult) => void;
  const install = vi.spyOn(port, "installMcp").mockImplementation(() => new Promise((resolve) => { finish = resolve; }));
  expect(radios.map((radio) => radio.tabIndex)).toEqual([0, -1, -1]);
  radios[0].focus();
  for (const [key, next] of [["ArrowRight", 1], ["ArrowDown", 2], ["ArrowRight", 0], ["ArrowLeft", 2], ["ArrowUp", 1]] as const) {
    await user.keyboard(`{${key}}`);
    expect(document.activeElement).toBe(radios[next]);
    expect(radios.map((radio) => radio.getAttribute("aria-checked"))).toEqual(radios.map((_, index) => String(index === next)));
    expect(radios.filter((radio) => radio.tabIndex === 0)).toEqual([radios[next]]);
  }
  expect(install).not.toHaveBeenCalled();
  await user.tab();
  expect(document.activeElement).toBe(screen.getByRole("button", { name: t("返回") }));
  await user.click(screen.getByRole("button", { name: t("接入") }));
  expect(install).toHaveBeenCalledExactlyOnceWith(draft.servers[0], "local");
  expect(radios.every((radio) => radio.disabled)).toBe(true);
  await user.keyboard("{ArrowRight}{ArrowDown}");
  expect(radios[1].getAttribute("aria-checked")).toBe("true");
  await act(async () => finish(ready));
  expect(installed).toHaveBeenCalledTimes(1);
  expect(close).not.toHaveBeenCalled();
  expect(screen.getByRole("button", { name: t("完成") })).toBeTruthy();
});

it.each(["zh", "en"])("skips unavailable project scopes and retains ordinary activation (%s)", async (lang) => {
  const { port, user, radios } = await setup(lang, false);
  const install = vi.spyOn(port, "installMcp").mockResolvedValue(ready);
  expect(radios.slice(1).every((radio) => radio.disabled)).toBe(true);
  radios[0].focus();
  await user.keyboard("{ArrowRight}{ArrowDown}{ArrowLeft}{ArrowUp}{End}{Home}");
  expect(document.activeElement).toBe(radios[0]);
  expect(radios[0].getAttribute("aria-checked")).toBe("true");
  await user.keyboard("{Enter} ");
  expect(install).not.toHaveBeenCalled();
  await user.click(screen.getByRole("button", { name: t("接入") }));
  expect(install).toHaveBeenCalledExactlyOnceWith(draft.servers[0], "user");
});

it.each(["zh", "en"])("selects the enabled scope boundaries with Home and End without installing (%s)", async (lang) => {
  const { port, user, radios } = await setup(lang);
  const install = vi.spyOn(port, "installMcp").mockResolvedValue(ready);
  radios[0].focus();
  await user.keyboard("{ArrowRight}");
  await user.keyboard("{Control>}{Home}{End}{/Control}{Meta>}{Home}{End}{/Meta}{Alt>}{Home}{End}{/Alt}");
  expect(document.activeElement).toBe(radios[1]);
  expect(radios[1].getAttribute("aria-checked")).toBe("true");
  for (const [key, next] of [["End", 2], ["End", 2], ["Home", 0], ["Home", 0], ["End", 2]] as const) {
    await user.keyboard(`{${key}}`);
    expect(document.activeElement).toBe(radios[next]);
    expect(radios.map((radio) => radio.getAttribute("aria-checked"))).toEqual(radios.map((_, index) => String(index === next)));
    expect(radios.filter((radio) => radio.tabIndex === 0)).toEqual([radios[next]]);
  }
  expect(install).not.toHaveBeenCalled();
  await user.tab();
  expect(document.activeElement).toBe(screen.getByRole("button", { name: t("返回") }));
  await user.click(screen.getByRole("button", { name: t("接入") }));
  expect(install).toHaveBeenCalledExactlyOnceWith(draft.servers[0], "project");
});

it.each(["zh", "en"])("leaves modified arrows alone and keeps scope choice available after a failed install (%s)", async (lang) => {
  const { port, user, radios } = await setup(lang);
  const install = vi.spyOn(port, "installMcp").mockRejectedValueOnce(new Error("offline")).mockResolvedValue(ready);
  radios[0].focus();
  await user.keyboard("{Control>}{ArrowRight}{End}{/Control}{Meta>}{ArrowDown}{End}{/Meta}{Alt>}{ArrowLeft}{End}{/Alt}");
  expect(document.activeElement).toBe(radios[0]);
  expect(radios[0].getAttribute("aria-checked")).toBe("true");
  await user.keyboard("{ArrowLeft}");
  await user.click(screen.getByRole("button", { name: t("接入") }));
  expect(await screen.findByText("offline")).toBeTruthy();
  expect(install).toHaveBeenNthCalledWith(1, draft.servers[0], "project");
  expect(radios.every((radio) => !radio.disabled)).toBe(true);
  radios[2].focus();
  await user.keyboard("{ArrowRight}");
  expect(document.activeElement).toBe(radios[0]);
  await user.click(screen.getByRole("button", { name: t("接入") }));
  expect(install).toHaveBeenNthCalledWith(2, draft.servers[0], "user");
});

it.each(["zh", "en"])("keeps all scopes locked after a partial save (%s)", async (lang) => {
  const { port, user } = await setup(lang);
  vi.spyOn(port, "parseMcp").mockResolvedValue({ servers: [...draft.servers, { ...draft.servers[0], name: "other" }], risks: [] });
  await user.click(screen.getByRole("button", { name: t("返回") }));
  await user.click(screen.getByRole("button", { name: t("查看内容") }));
  const current = screen.getAllByRole<HTMLButtonElement>("radio");
  const install = vi.spyOn(port, "installMcp").mockResolvedValueOnce(ready).mockRejectedValueOnce(new Error("offline"));
  current[0].focus();
  await user.keyboard("{ArrowRight}");
  await user.click(screen.getByRole("button", { name: t("接入") }));
  expect(await screen.findByText("offline")).toBeTruthy();
  expect(current.every((radio) => radio.disabled)).toBe(true);
  await user.keyboard("{ArrowLeft}{ArrowUp}{End}{Home}");
  expect(current[1].getAttribute("aria-checked")).toBe("true");
  expect(install.mock.calls.map((call) => call[1])).toEqual(["local", "local"]);
});
