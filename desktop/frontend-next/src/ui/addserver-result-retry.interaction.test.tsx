// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { AddServer } from "./AddServer";
import { MockPort } from "../port/mock";
import type { AgentPort, McpDraft, McpInstallResult } from "../port/port";
import { boot, STORAGE } from "../i18n";

const setLocale = (lang: "zh" | "en") => { localStorage.setItem(STORAGE, lang); boot(); };

afterEach(() => { cleanup(); setLocale("zh"); });

const draft: McpDraft = {
  servers: ["docs", "search"].map((name) => ({ name, transport: "http", url: `https://example.test/${name}` })),
  risks: [],
};
const result = (name: string, state: string): McpInstallResult => ({
  name, state, toolCount: state === "ready" ? 3 : 0,
  action: state === "issue" ? "retry" : state === "action_required" ? "authenticate" : "none",
  message: state === "issue" ? `${name} could not connect` : "",
});

describe("structured MCP installation failures", () => {
  it.each(["zh", "en"] as const)("keeps failed %s results retryable and never repeats saved servers", async (locale) => {
    setLocale(locale);
    const port = new MockPort() as unknown as AgentPort;
    vi.spyOn(port, "parseMcp").mockResolvedValue(draft);
    const install = vi.spyOn(port, "installMcp")
      .mockResolvedValueOnce(result("docs", "ready"))
      .mockResolvedValueOnce(result("search", "issue"));
    const onInstalled = vi.fn();
    const onClose = vi.fn();
    render(<AddServer port={port} canProject onClose={onClose} onInstalled={onInstalled} />);
    await userEvent.type(screen.getByRole("textbox"), "https://example.test/batch");
    await userEvent.click(screen.getByRole("button", { name: locale === "zh" ? "查看内容" : "See what it is" }));
    await userEvent.click(await screen.findByRole("button", { name: locale === "zh" ? "接入" : "Connect" }));
    await screen.findByText("search could not connect");
    expect(document.querySelector('.outcome[data-state="ready"] .nm')?.textContent).toBe("docs");
    expect([...document.querySelectorAll(".cand .nm")].map((n) => n.textContent)).toEqual(["search"]);
    expect(screen.queryByRole("button", { name: locale === "zh" ? "完成" : "Done" })).toBeNull();
    expect(onInstalled).toHaveBeenCalledTimes(1);
    for (const radio of screen.getAllByRole<HTMLButtonElement>("radio")) expect(radio.disabled).toBe(true);

    let finish!: (value: McpInstallResult) => void;
    const pending = new Promise<McpInstallResult>((resolve) => { finish = resolve; });
    install.mockImplementationOnce(() => pending);
    await userEvent.click(screen.getByRole("button", { name: locale === "zh" ? "接入" : "Connect" }));
    const retry = screen.getByRole<HTMLButtonElement>("button", { name: locale === "zh" ? "连接中…" : "Connecting…" });
    expect(retry.disabled).toBe(true);
    await userEvent.click(retry);
    expect(install.mock.calls.map(([server]) => server.name)).toEqual(["docs", "search", "search"]);
    await act(async () => { finish(result("search", "ready")); await pending; });
    const done = await screen.findByRole("button", { name: locale === "zh" ? "完成" : "Done" });
    expect(document.querySelector('.outcome[data-state="issue"]')).toBeNull();
    expect([...document.querySelectorAll(".outcome .nm")].map((n) => n.textContent)).toEqual(["docs", "search"]);
    expect(onInstalled).toHaveBeenCalledTimes(2);
    await userEvent.click(done);
    expect(onClose).toHaveBeenCalledTimes(1);
    expect(install).toHaveBeenCalledTimes(3);
  });

  it.each(["issue", "action_required"])("preserves a %s result until the remaining failures recover", async (firstState) => {
    const port = new MockPort() as unknown as AgentPort;
    vi.spyOn(port, "parseMcp").mockResolvedValue(draft);
    const install = vi.spyOn(port, "installMcp")
      .mockResolvedValueOnce(result("docs", firstState))
      .mockResolvedValueOnce(result("search", "issue"));
    const onInstalled = vi.fn();
    render(<AddServer port={port} canProject onClose={() => {}} onInstalled={onInstalled} />);
    await userEvent.type(screen.getByRole("textbox"), "https://example.test/batch");
    await userEvent.click(screen.getByRole("button", { name: "查看内容" }));
    await userEvent.click(await screen.findByRole("button", { name: "接入" }));
    await screen.findByText("search could not connect");
    const saved = firstState !== "issue";
    expect(onInstalled).toHaveBeenCalledTimes(saved ? 1 : 0);
    for (const radio of screen.getAllByRole<HTMLButtonElement>("radio")) expect(radio.disabled).toBe(saved);
    install.mockImplementation(async (server) => result(server.name, "ready"));
    await userEvent.click(screen.getByRole("button", { name: "接入" }));
    await screen.findByRole("button", { name: "完成" });
    expect(install.mock.calls.map(([server]) => server.name)).toEqual(saved ? ["docs", "search", "search"] : ["docs", "search", "docs", "search"]);
    expect(document.querySelector('.outcome[data-state="issue"]')).toBeNull();
    expect(document.querySelectorAll(".outcome")).toHaveLength(2);
    expect(onInstalled).toHaveBeenCalledTimes(saved ? 2 : 1);
  });
});
