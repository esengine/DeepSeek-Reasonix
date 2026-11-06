// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
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
    const pro = await screen.findByRole("radio", { name: /deepseek-v4-pro/ });
    expect(screen.queryByRole("radiogroup", { name: "协议" })).toBeNull();
    expect(pro.getAttribute("aria-checked")).toBe("true");
    await user.click(screen.getByRole("radio", { name: /deepseek-flash/ }));
    await user.click(screen.getByRole("button", { name: "继续" }));
    expect(save).toHaveBeenCalledWith(expect.objectContaining({ kind: "openai", baseUrl: "https://api.deepseek.com", default: "deepseek-flash", replace: true }));
    await waitFor(() => expect(onDone).toHaveBeenCalled());
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

describe("the project step", () => {
  function drawWithProject() {
    const port = new MockPort() as unknown as AgentPort;
    port.saveProvider = vi.fn(async () => {});
    const onDone = vi.fn();
    const add = vi.fn(async (_dir: string) => {});
    const pick = vi.fn(async () => "/work/demo");
    render(<Onboarding port={port} setup={{ required: true, provider: "", model: "" }} onDone={onDone} project={{ pick, add }} />);
    return { onDone, add, pick, user: userEvent.setup() };
  }

  it("asks which folder to work in after the model, and starts only once one is chosen", async () => {
    const { onDone, add, user } = drawWithProject();
    await user.click(screen.getByRole("button", { name: "开始设置" }));
    await user.type(screen.getByPlaceholderText("sk-…"), "sk-test");
    await user.click(screen.getByRole("button", { name: "连接" }));
    await user.click(await screen.findByRole("button", { name: "继续" }, { timeout: 5000 }));
    expect(await screen.findByRole("heading", { name: "在哪个项目里干活？" })).toBeTruthy();
    const start = screen.getByRole("button", { name: "开始" }) as HTMLButtonElement;
    expect(start.disabled).toBe(true);
    await user.click(screen.getByRole("button", { name: "选择文件夹…" }));
    expect(await screen.findByText("demo")).toBeTruthy();
    expect(start.disabled).toBe(false);
    await user.click(start);
    await waitFor(() => expect(add).toHaveBeenCalledWith("/work/demo"));
    await waitFor(() => expect(onDone).toHaveBeenCalled());
  });
});
