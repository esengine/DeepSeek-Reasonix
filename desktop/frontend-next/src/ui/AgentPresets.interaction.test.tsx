// @vitest-environment jsdom
import { cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, it, expect, vi } from "vitest";
import { MockPort } from "../port/mock";
import { AgentPresets } from "./AgentPresets";

afterEach(cleanup);

describe("Agent preset management", () => {
  it("shows only Standard and Minimal built-in cards and changes the default without changing this conversation", async () => {
    const user = userEvent.setup();
    const port = new MockPort();
    render(<AgentPresets port={port} />);
    expect(await screen.findByRole("button", { name: "标准模式" })).toBeTruthy();
    const minimal = screen.getByRole("button", { name: "精简模式" });
    expect(document.querySelectorAll(".composition-card")).toHaveLength(2);
    expect(screen.queryByText("代码审查")).toBeNull();
    expect(screen.queryByText("资料研究")).toBeNull();
    await user.click(within(minimal.closest("article")!).getByRole("button", { name: "设为个人默认" }));
    await waitFor(async () => expect((await port.agentPresets()).default).toBe("builtin:minimal"));
    expect((await port.agentComposition())?.id).toBe("builtin:general");
    await user.click(minimal);
    await user.click(screen.getByRole("button", { name: "复制为自定义预设" }));
    await user.click(screen.getByRole("button", { name: "保存预设" }));
    await screen.findByRole("status");
    const copied = (await port.agentPresets()).presets.find((v) => v.definition.id === "user:custom")!;
    expect(copied.definition.tools.members.map((m) => m.id)).toEqual(["bash", "view_image"]);
    expect(copied.definition.mcp).toEqual({ mode: "select", members: [] });
  });
  it("copies a built-in, saves literal fields and keeps the current session revision", async () => {
    const user = userEvent.setup();
    const port = new MockPort();
    render(<AgentPresets port={port} />);
    await user.click(await screen.findByRole("button", { name: "复制为自定义预设" }));
    await user.clear(screen.getByLabelText("预设名称"));
    await user.type(screen.getByLabelText("预设名称"), "My agent");
    await user.type(screen.getByLabelText("系统提示词开头"), "Literal prefix");
    await user.type(screen.getByLabelText("第一方指令之后"), "Literal suffix");
    await user.click(screen.getByRole("switch", { name: "用前缀替换整个系统提示词（complete）" }));
    await user.click(screen.getByRole("switch", { name: "注入运行环境与离线提示" }));
    await user.click(screen.getByRole("button", { name: "保存预设" }));
    expect((await screen.findByRole("status")).textContent).toContain("新会话使用新版");
    expect((await port.agentPresets()).presets.some((v) => v.definition.name === "My agent")).toBe(true);
    expect((await port.agentComposition())?.id).toBe("builtin:general");
    expect((await port.agentPresets()).presets.find((v)=>v.definition.name==="My agent")?.definition.persona).toEqual({prefix:"Literal prefix",suffix:"Literal suffix",complete:true,include_runtime_context:false});
  });
  it("keeps an unsaved draft after a conflict", async () => {
    const user = userEvent.setup();
    const port = new MockPort();
    vi.spyOn(port, "saveAgentPreset").mockRejectedValue(new Error("conflict"));
    render(<AgentPresets port={port} />);
    await user.click(await screen.findByRole("button", { name: "复制为自定义预设" }));
    await user.type(screen.getByLabelText("预设名称"), " draft");
    await user.click(screen.getByRole("button", { name: "保存预设" }));
    await waitFor(() => expect(screen.getByRole("alert")).toBeTruthy());
    expect((screen.getByLabelText("预设名称") as HTMLInputElement).value).toContain("draft");
  });
  it("does not save an earlier parameter value when the visible JSON is invalid", async () => {
    const user=userEvent.setup();const port=new MockPort();const save=vi.spyOn(port,"saveAgentPreset");
    render(<AgentPresets port={port}/>);
    await user.click(await screen.findByRole("button",{name:"复制为自定义预设"}));
    await user.selectOptions(screen.getByLabelText("MCP 服务 选择方式"),"select");
    const group=screen.getByRole("group",{name:"MCP 服务"});
    await user.click(group.querySelector("button")!);
    await user.type(screen.getByLabelText("MCP 服务 ID 1"),"docs");
    await user.clear(screen.getByLabelText("MCP 服务 独立参数 1"));
    await user.type(screen.getByLabelText("MCP 服务 独立参数 1"),"invalid");
    expect(screen.getByRole<HTMLButtonElement>("button",{name:"保存预设"}).disabled).toBe(true);
    expect(save).not.toHaveBeenCalled();
    await user.click(group.querySelector(".composition-module button")!);
    await waitFor(()=>expect(screen.getByRole<HTMLButtonElement>("button",{name:"保存预设"}).disabled).toBe(false));
  });
});
