// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { MockPort } from "../port/mock";
import type { AgentPort, ProviderDraft } from "../port/port";
import { Onboarding } from "./Onboarding";

afterEach(cleanup);

function draw(onSkip?: () => void) {
  const port = new MockPort() as unknown as AgentPort;
  const save = vi.fn(async (_draft: ProviderDraft) => {});
  port.saveProvider = save;
  const onDone = vi.fn();
  render(<Onboarding port={port} setup={{ required: true, provider: "", model: "" }} onDone={onDone} onSkip={onSkip} />);
  return { port, save, onDone, user: userEvent.setup() };
}

describe("first-run setup", () => {
  it("walks from the welcome through a listed service to a default model, without asking how it is spoken to", async () => {
    const { save, onDone, user } = draw();
    expect(screen.getByRole("heading", { name: "欢迎使用 Reasonix" })).toBeTruthy();
    await user.click(screen.getByRole("button", { name: "开始设置" }));
    expect(screen.getByRole("radio", { name: /DeepSeek/ }).getAttribute("aria-checked")).toBe("true");
    expect(screen.queryByLabelText("服务地址")).toBeNull();
    expect(screen.getByText(/服务地址已按 DeepSeek 填好（api.deepseek.com）/)).toBeTruthy();
    await user.type(screen.getByPlaceholderText("sk-…"), "sk-test");
    await user.click(screen.getByRole("button", { name: "连接" }));
    expect(await screen.findByText("已连上 DeepSeek · 找到 2 个模型")).toBeTruthy();
    expect(screen.queryByRole("radiogroup", { name: "协议" })).toBeNull();
    expect(screen.getByRole("radio", { name: /deepseek-v4-pro/ }).getAttribute("aria-checked")).toBe("true");
    await user.click(screen.getByRole("radio", { name: /deepseek-flash/ }));
    await user.click(screen.getByRole("button", { name: "继续" }));
    expect(save).toHaveBeenCalledWith(expect.objectContaining({ kind: "openai", baseUrl: "https://api.deepseek.com", default: "deepseek-flash", replace: true }));
    expect(onDone).toHaveBeenCalled();
  });

  it("asks a typed address how it is spoken to when it answers both ways, and saves the one chosen", async () => {
    const { save, user } = draw();
    await user.click(screen.getByRole("button", { name: "开始设置" }));
    await user.click(screen.getByRole("radio", { name: /中转站 \/ 自建/ }));
    await user.type(screen.getByLabelText("服务地址"), "https://relay.example/v1");
    await user.type(screen.getByPlaceholderText("sk-…"), "sk-test");
    await user.click(screen.getByRole("button", { name: "连接" }));
    const responses = await screen.findByRole("radio", { name: /Responses/ });
    await user.click(responses);
    await user.click(screen.getByRole("button", { name: "继续" }));
    expect(save).toHaveBeenCalledWith(expect.objectContaining({ kind: "responses", baseUrl: "https://relay.example/v1" }));
  });

  it("goes back a step without losing the key, and can be left for later from any step", async () => {
    const onSkip = vi.fn();
    const { user } = draw(onSkip);
    await user.click(screen.getByRole("button", { name: "开始设置" }));
    await user.type(screen.getByPlaceholderText("sk-…"), "sk-test");
    await user.click(screen.getByRole("button", { name: "连接" }));
    await screen.findByRole("button", { name: "继续" });
    await user.click(screen.getByRole("button", { name: "上一步" }));
    expect((screen.getByPlaceholderText("sk-…") as HTMLInputElement).value).toBe("sk-test");
    await user.click(screen.getByRole("button", { name: "稍后再说" }));
    expect(onSkip).toHaveBeenCalled();
  });
});
