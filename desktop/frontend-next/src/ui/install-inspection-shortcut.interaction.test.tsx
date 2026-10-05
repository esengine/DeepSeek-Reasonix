// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { act, cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { AddPlugin } from "./AddPlugin";
import { AddServer } from "./AddServer";
import { MockPort } from "../port/mock";
import type { AgentPort, McpDraft } from "../port/port";

afterEach(cleanup);

const source = "https://example.test/review-kit";
const failure = "source could not be read";
const forms = ["plugin", "mcp"] as const;
const modifiers = ["Control", "Meta"] as const;

function deferred() {
  let finish!: () => void;
  let fail!: (error: Error) => void;
  const promise = new Promise<void>((resolve, reject) => { finish = resolve; fail = reject; });
  return { promise, finish, fail: () => fail(new Error(failure)) };
}

async function draw(form: typeof forms[number]) {
  const port = new MockPort() as unknown as AgentPort;
  const plan = await port.planPlugin({ source });
  const draft: McpDraft = { servers: [{ name: "docs", transport: "http", url: "https://example.test/mcp" }], risks: [] };
  const pluginRead = vi.spyOn(port, "planPlugin").mockResolvedValue(plan);
  const mcpRead = vi.spyOn(port, "parseMcp").mockResolvedValue(draft);
  const pluginInstall = vi.spyOn(port, "installPlugin");
  const mcpInstall = vi.spyOn(port, "installMcp");
  const onInstalled = vi.fn();
  render(form === "plugin"
    ? <AddPlugin port={port} onClose={() => {}} onInstalled={onInstalled} />
    : <AddServer port={port} canProject onClose={() => {}} onInstalled={onInstalled} />);
  return { plan, draft, pluginRead, mcpRead, read: form === "plugin" ? pluginRead : mcpRead, pluginInstall, mcpInstall, onInstalled };
}

it.each(forms.flatMap((form) => modifiers.flatMap((modifier) => ["", "   "].map((text) => [form, modifier, text] as const))))(
  "ignores %s %s+Enter for empty input %j", async (form, modifier, text) => {
    const { read, pluginInstall, mcpInstall } = await draw(form);
    const input = screen.getByRole<HTMLTextAreaElement>("textbox");
    if (text) await userEvent.type(input, text);
    else await userEvent.click(input);
    expect(screen.getByRole<HTMLButtonElement>("button", { name: "查看内容" }).disabled).toBe(true);
    await userEvent.keyboard(`{${modifier}>}{Enter}{/${modifier}}`);
    expect(read).not.toHaveBeenCalled();
    expect(input.value).toBe(text);
    expect(pluginInstall).not.toHaveBeenCalled();
    expect(mcpInstall).not.toHaveBeenCalled();
  },
);

it.each(forms.flatMap((form) => modifiers.flatMap((modifier) => ["success", "failure"].map((outcome) => [form, modifier, outcome] as const))))(
  "keeps %s inspection single-flight and its input unchanged for %s+Enter (%s)", async (form, modifier, outcome) => {
    const { read, pluginRead, mcpRead, plan, draft, pluginInstall, mcpInstall, onInstalled } = await draw(form);
    const pending = deferred();
    pluginRead.mockImplementationOnce(async () => { await pending.promise; return plan; });
    mcpRead.mockImplementationOnce(async () => { await pending.promise; return draft; });
    const input = screen.getByRole<HTMLTextAreaElement>("textbox");
    await userEvent.type(input, source);
    await userEvent.click(screen.getByRole("button", { name: "查看内容" }));
    const reading = screen.getByRole<HTMLButtonElement>("button", { name: "读取中…" });
    expect(reading.disabled).toBe(true);
    await userEvent.click(input);
    await userEvent.type(input, " changed");
    await userEvent.keyboard(`{${modifier}>}{Enter}{Enter}{/${modifier}}`);
    expect(read).toHaveBeenCalledTimes(1);
    expect(reading.disabled).toBe(true);
    expect(input.value).toBe(source);
    await act(async () => outcome === "success" ? pending.finish() : pending.fail());
    if (outcome === "failure") {
      expect(screen.getByText(failure)).toBeTruthy();
      expect(screen.getByRole<HTMLButtonElement>("button", { name: "查看内容" }).disabled).toBe(false);
    } else {
      expect(screen.getByRole("button", { name: form === "plugin" ? "安装" : "接入" })).toBeTruthy();
      await userEvent.click(screen.getByRole("button", { name: "返回" }));
    }
    await userEvent.click(screen.getByRole("textbox"));
    await userEvent.keyboard(`{${modifier}>}{Enter}{/${modifier}}`);
    expect(read).toHaveBeenCalledTimes(2);
    if (form === "plugin") expect(pluginRead).toHaveBeenLastCalledWith({ source, name: undefined, replace: false, planId: undefined });
    else expect(mcpRead).toHaveBeenLastCalledWith(source);
    expect(screen.getByRole<HTMLButtonElement>("button", { name: form === "plugin" ? "安装" : "接入" }).disabled).toBe(false);
    expect(pluginInstall).not.toHaveBeenCalled();
    expect(mcpInstall).not.toHaveBeenCalled();
    expect(onInstalled).not.toHaveBeenCalled();
  },
);

it.each(forms)("keeps ordinary Enter as multiline input for %s", async (form) => {
  const { read } = await draw(form);
  const input = screen.getByRole<HTMLTextAreaElement>("textbox");
  await userEvent.type(input, "first{Enter}second");
  expect(input.value).toBe("first\nsecond");
  expect(read).not.toHaveBeenCalled();
});
