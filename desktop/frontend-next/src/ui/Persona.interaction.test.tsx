// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { Persona } from "./Persona";
import { HttpError, type AgentPort } from "../port/port";
import type { PersonaDraft, PersonaSettings } from "../port/boundary";

afterEach(cleanup);

const base: PersonaSettings = {
  prefix: "", suffix: "", complete: false, include_runtime_context: true,
  path: "/u/config.toml",
  presets: [
    { id: "engineer", prefix: "Rigorous engineer prefix.", suffix: "", complete: true, include_runtime_context: true },
    { id: "concise", prefix: "", suffix: "Be terse.", complete: false, include_runtime_context: false },
  ],
};

function portWith(initial: PersonaSettings = base) {
  return {
    persona: vi.fn(async () => initial),
    savePersonaSettings: vi.fn(async (s: PersonaDraft) => ({ ...initial, ...s })),
  } as unknown as AgentPort & { savePersonaSettings: ReturnType<typeof vi.fn> };
}

const field = (name: string) => screen.findByRole<HTMLTextAreaElement>("textbox", { name });
const saveButton = () => screen.getByRole<HTMLButtonElement>("button", { name: "保存" });
const now = (container: HTMLElement) => container.querySelector(".grp .grp-hd .now")?.textContent ?? null;

describe("the system persona page", () => {
  it("renders the presets the kernel serves and fills both boxes from one", async () => {
    const { container } = render(<Persona port={portWith()} />);
    expect(await screen.findByRole("button", { name: "严谨工程师" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "简洁" })).toBeTruthy();

    await userEvent.click(screen.getByRole("button", { name: "严谨工程师" }));
    expect((await field("系统提示词开头")).value).toBe("Rigorous engineer prefix.");
    expect((await field("第一方指令之后")).value).toBe("");
    expect(screen.getByRole("switch", { name: "用前缀替换整个系统提示词（complete）" }).getAttribute("aria-checked")).toBe("true");
    expect(now(container)).toBe("严谨工程师");

    await userEvent.click(screen.getByRole("button", { name: "简洁" }));
    expect((await field("系统提示词开头")).value).toBe("");
    expect((await field("第一方指令之后")).value).toBe("Be terse.");
    // The runtime-context switch shows the inverse of the field it writes.
    expect(screen.getByRole("switch", { name: "不注入可选运行时上下文" }).getAttribute("aria-checked")).toBe("true");
  });

  it("reads 自定义 once the applied preset is edited", async () => {
    const { container } = render(<Persona port={portWith()} />);
    await userEvent.click(await screen.findByRole("button", { name: "严谨工程师" }));
    expect(now(container)).toBe("严谨工程师");
    await userEvent.type(await field("系统提示词开头"), " Also be kind.");
    expect(now(container)).toBe("自定义");
  });

  it("saves all four fields and says only new sessions read them", async () => {
    const port = portWith();
    render(<Persona port={port} />);
    const prefix = await field("系统提示词开头");
    expect(saveButton().disabled).toBe(true);

    await userEvent.type(prefix, "Lead with evidence.");
    await userEvent.type(await field("第一方指令之后"), "Stay terse.");
    await userEvent.click(screen.getByRole("switch", { name: "不注入可选运行时上下文" }));

    expect(saveButton().disabled).toBe(false);
    await userEvent.click(saveButton());
    expect(port.savePersonaSettings).toHaveBeenCalledWith({
      prefix: "Lead with evidence.",
      suffix: "Stay terse.",
      complete: false,
      include_runtime_context: false,
    });
    expect(await screen.findByText("已保存，新会话生效")).toBeTruthy();
    expect(screen.getByText("/u/config.toml")).toBeTruthy();
    // Clean again: the same bytes would be a second no-op save.
    expect(saveButton().disabled).toBe(true);
  });

  it("cannot turn complete on until there is a prefix", async () => {
    render(<Persona port={portWith()} />);
    const sw = (await screen.findByRole("switch", { name: "用前缀替换整个系统提示词（complete）" })) as HTMLButtonElement;
    expect(sw.disabled).toBe(true);
    expect(screen.getByText(/先填写前缀才能开启/)).toBeTruthy();
    await userEvent.type(await field("系统提示词开头"), "You are someone.");
    await waitFor(() => expect(sw.disabled).toBe(false));
    await userEvent.click(sw);
    expect(sw.getAttribute("aria-checked")).toBe("true");
  });

  it("refuses complete with an empty prefix without writing", async () => {
    const port = portWith();
    render(<Persona port={port} />);
    await userEvent.click(await screen.findByRole("button", { name: "严谨工程师" }));
    await userEvent.clear(await field("系统提示词开头"));
    await userEvent.click(saveButton());
    expect(port.savePersonaSettings).not.toHaveBeenCalled();
    expect(await screen.findByText("启用「用前缀替换整个系统提示词」时，前缀不能为空。")).toBeTruthy();
  });

  it("shows the kernel's wording for the two persona refusals", async () => {
    const port = portWith();
    render(<Persona port={port} />);
    await userEvent.type(await field("系统提示词开头"), "Something.");

    port.savePersonaSettings.mockImplementationOnce(async () => {
      throw new HttpError(400, "persona.complete_requires_prefix", {
        code: "persona.complete_requires_prefix", params: { detail: "prefix is blank" },
      });
    });
    await userEvent.click(saveButton());
    expect(await screen.findByText(/complete 模式必须填写前缀/)).toBeTruthy();

    port.savePersonaSettings.mockImplementationOnce(async () => {
      throw new HttpError(400, "persona.too_large", {
        code: "persona.too_large", params: { detail: "70 KiB" },
      });
    });
    await userEvent.click(saveButton());
    expect(await screen.findByText(/人设内容超出长度上限/)).toBeTruthy();
  });

  it("renders the empty state when the settings cannot be read", async () => {
    const port = { persona: vi.fn(async () => { throw new Error("offline"); }), savePersonaSettings: vi.fn() } as unknown as AgentPort;
    render(<Persona port={port} />);
    expect(await screen.findByText("无法读取人设设置。")).toBeTruthy();
  });
});
