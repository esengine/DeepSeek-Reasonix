// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { cleanup, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { PublishForm } from "./MarketPublish";
import { MockPort } from "../port/mock";
import { boot, STORAGE, t } from "../i18n";
import type { AgentPort } from "../port/port";

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  localStorage.setItem(STORAGE, "zh");
  boot();
});

const nameField = () => screen.getByLabelText<HTMLInputElement>(new RegExp("^" + t("名称")));
const sourceField = () => screen.getByLabelText<HTMLInputElement>(new RegExp("^" + t("来源地址")));
const types = () => within(screen.getByRole("radiogroup", { name: t("类型") }));
const radio = (name: string) => types().getByRole<HTMLButtonElement>("radio", { name: t(name) });

function draw(lang = "zh") {
  localStorage.setItem(STORAGE, lang);
  boot();
  const port = new MockPort() as unknown as AgentPort;
  render(<PublishForm port={port} handle="demo" onMine={() => {}} />);
  return port;
}

it.each(["zh", "en"])("leaves the selected kind in one Tab stop and returns to it (%s)", async (lang) => {
  draw(lang);
  await userEvent.click(radio("插件"));
  await userEvent.tab();
  expect(document.activeElement).toBe(nameField());
  await userEvent.tab({ shift: true });
  expect(document.activeElement).toBe(radio("插件"));
});

it("publishes the kind chosen entirely with the keyboard", async () => {
  const port = draw();
  const publish = vi.spyOn(port, "publishMarket").mockRejectedValue(new Error("registry unavailable"));
  await userEvent.tab();
  await userEvent.keyboard("{ArrowRight}{ArrowRight}{ArrowRight}");
  expect(document.activeElement).toBe(radio("主题"));
  await userEvent.tab();
  expect(document.activeElement).toBe(nameField());
  await userEvent.keyboard("notes-kit");
  await userEvent.tab();
  await userEvent.tab();
  expect(document.activeElement).toBe(sourceField());
  const source = "https://github.com/demo/notes-kit/tree/" + "a".repeat(40) + "/theme";
  await userEvent.keyboard(source);
  for (let i = 0; i < 6; i++) await userEvent.tab();
  expect(document.activeElement).toBe(screen.getByRole("button", { name: t("提交审核") }));
  await userEvent.keyboard("{Enter}");
  expect(publish).toHaveBeenCalledExactlyOnceWith({
    kind: "theme", name: "notes-kit", source, summary: "", description: "", repoUrl: "", version: "", tags: [], visibility: "public",
  });
  expect(await screen.findByText("registry unavailable")).toBeTruthy();
});

it("keeps mouse and Space selection available", async () => {
  draw();
  await userEvent.click(radio("主题"));
  expect(radio("主题").getAttribute("aria-checked")).toBe("true");
  radio("MCP 服务").focus();
  await userEvent.keyboard(" ");
  expect(radio("MCP 服务").getAttribute("aria-checked")).toBe("true");
});
