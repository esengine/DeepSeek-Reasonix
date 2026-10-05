// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { AddServer } from "./AddServer";
import { MockPort } from "../port/mock";
import type { AgentPort, McpDraft, McpInstallResult } from "../port/port";

afterEach(cleanup);

const draft: McpDraft = {
  servers: ["docs", "search", "notes"].map((name) => ({ name, transport: "http", url: `https://example.test/${name}` })),
  risks: [],
};
const result = (name: string, state: string): McpInstallResult => ({ name, state, toolCount: 1, action: "installed", message: "" });

async function setup() {
  const port = new MockPort() as unknown as AgentPort;
  const parse = vi.spyOn(port, "parseMcp").mockResolvedValue(draft);
  const install = vi.spyOn(port, "installMcp").mockImplementation(async (s) => result(s.name, "ready"));
  const onInstalled = vi.fn();
  render(<AddServer port={port} canProject onClose={() => {}} onInstalled={onInstalled} />);
  await userEvent.type(screen.getByRole("textbox"), "https://example.test/batch");
  await userEvent.click(screen.getByRole("button", { name: "查看内容" }));
  await screen.findByRole("button", { name: "接入" });
  return { parse, install, onInstalled };
}

describe("partial MCP batch retries", () => {
  it.each(["ready", "action_required"])("shows a saved %s result and retries only the remaining servers", async (state) => {
    const { install, onInstalled } = await setup();
    install.mockResolvedValueOnce(result("docs", state)).mockRejectedValueOnce(new Error("search save failed"));
    await userEvent.click(screen.getByRole("button", { name: "接入" }));
    expect(await screen.findByText("search save failed")).toBeTruthy();
    expect(document.querySelector(`.outcome[data-state="${state}"] .nm`)?.textContent).toBe("docs");
    expect([...document.querySelectorAll(".cand .nm")].map((n) => n.textContent)).toEqual(["search", "notes"]);
    expect(onInstalled).toHaveBeenCalledTimes(1);
    await userEvent.click(screen.getByRole("button", { name: "接入" }));
    await screen.findByRole("button", { name: "完成" });
    expect(install.mock.calls.map(([s]) => s.name)).toEqual(["docs", "search", "search", "notes"]);
    expect([...document.querySelectorAll(".outcome .nm")].map((n) => n.textContent)).toEqual(["docs", "search", "notes"]);
    expect(onInstalled).toHaveBeenCalledTimes(2);
  });

  it.each(["ready", "action_required"])("retains a saved %s result through a pending retry and another exception", async (state) => {
    const { install, onInstalled } = await setup();
    let reject!: (error: Error) => void;
    const pending = new Promise<McpInstallResult>((_, no) => { reject = no; });
    install.mockResolvedValueOnce(result("docs", state)).mockRejectedValueOnce(new Error("first failure"))
      .mockImplementationOnce(() => pending);
    await userEvent.click(screen.getByRole("button", { name: "接入" }));
    await screen.findByText("first failure");
    await userEvent.click(screen.getByRole("button", { name: "接入" }));
    expect(document.querySelector(`.outcome[data-state="${state}"] .nm`)?.textContent).toBe("docs");
    expect(screen.getByRole<HTMLButtonElement>("button", { name: "连接中…" }).disabled).toBe(true);
    expect(screen.queryByRole("button", { name: "完成" })).toBeNull();
    await act(async () => { reject(new Error("second failure")); await pending.catch(() => {}); });
    expect(screen.getByText("second failure")).toBeTruthy();
    expect(document.querySelector(`.outcome[data-state="${state}"] .nm`)?.textContent).toBe("docs");
    expect(onInstalled).toHaveBeenCalledTimes(1);
    await userEvent.click(screen.getByRole("button", { name: "接入" }));
    await screen.findByRole("button", { name: "完成" });
    expect(install.mock.calls.map(([s]) => s.name)).toEqual(["docs", "search", "search", "search", "notes"]);
    expect(onInstalled).toHaveBeenCalledTimes(2);
  });

  it("retries issue results and replaces their old outcome without duplicating rows", async () => {
    const { install, onInstalled } = await setup();
    install.mockResolvedValueOnce(result("docs", "issue")).mockRejectedValueOnce(new Error("search failed"));
    await userEvent.click(screen.getByRole("button", { name: "接入" }));
    await screen.findByText("search failed");
    expect(document.querySelector('.outcome[data-state="issue"] .nm')?.textContent).toBe("docs");
    expect(screen.getByRole<HTMLButtonElement>("radio", { name: /仅当前项目/ }).disabled).toBe(false);
    expect(onInstalled).not.toHaveBeenCalled();
    await userEvent.click(screen.getByRole("button", { name: "接入" }));
    await screen.findByRole("button", { name: "完成" });
    expect(install.mock.calls.map(([s]) => s.name)).toEqual(["docs", "search", "docs", "search", "notes"]);
    expect(document.querySelector('.outcome[data-state="issue"]')).toBeNull();
    expect([...document.querySelectorAll(".outcome .nm")].map((n) => n.textContent)).toEqual(["docs", "search", "notes"]);
    expect(onInstalled).toHaveBeenCalledTimes(1);
  });

  it("keeps the confirmed destination after part of the batch has been saved", async () => {
    const { install } = await setup();
    await userEvent.click(screen.getByRole("radio", { name: /仅当前项目/ }));
    install.mockResolvedValueOnce(result("docs", "ready")).mockRejectedValueOnce(new Error("search failed"));
    await userEvent.click(screen.getByRole("button", { name: "接入" }));
    await screen.findByText("search failed");
    for (const radio of screen.getAllByRole<HTMLButtonElement>("radio")) expect(radio.disabled).toBe(true);
    expect(screen.getByRole("radio", { name: /仅当前项目/ }).getAttribute("aria-checked")).toBe("true");
    await userEvent.click(screen.getByRole("radio", { name: /写进仓库/ }));
    await userEvent.click(screen.getByRole("button", { name: "接入" }));
    await screen.findByRole("button", { name: "完成" });
    expect(install.mock.calls.map(([, scope]) => scope)).toEqual(["local", "local", "local", "local"]);
  });

  it("clears partial results when the user returns to edit and inspects a new source", async () => {
    const { parse, install } = await setup();
    install.mockResolvedValueOnce(result("docs", "ready")).mockRejectedValueOnce(new Error("old failure"));
    await userEvent.click(screen.getByRole("button", { name: "接入" }));
    await screen.findByText("old failure");
    await userEvent.click(screen.getByRole("button", { name: "返回" }));
    expect(screen.queryByText("old failure")).toBeNull();
    expect(document.querySelector(".outcome")).toBeNull();
    parse.mockResolvedValueOnce({ servers: [draft.servers[0]!], risks: [] });
    await userEvent.click(screen.getByRole("button", { name: "查看内容" }));
    await userEvent.click(await screen.findByRole("button", { name: "接入" }));
    await screen.findByRole("button", { name: "完成" });
    expect(install.mock.calls.map(([s]) => s.name)).toEqual(["docs", "search", "docs"]);
  });
});
