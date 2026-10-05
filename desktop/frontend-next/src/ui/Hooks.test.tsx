// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { boot, STORAGE } from "../i18n";
import type { AgentPort, HookCatalog } from "../port/port";
import { Hooks } from "./Hooks";

const fs = await vi.importActual<{ readFileSync: (path: string, encoding: string) => string }>("node:fs");
const kernel = fs.readFileSync("../../internal/ext/hook/hook.go", "utf8");

const LABELS: Record<string, [string, string]> = {
  PreToolUse: ["工具执行前", "Before a tool runs"],
  PostToolUse: ["工具执行后", "After a tool runs"],
  PostToolUseFailure: ["工具执行失败后", "After a tool fails"],
  PermissionRequest: ["请求权限时", "When permission is requested"],
  UserPromptSubmit: ["提交消息时", "When a message is submitted"],
  Stop: ["本轮结束时", "When the turn ends"],
  StopFailure: ["本轮失败时", "When the turn fails"],
  PostLLMCall: ["模型响应完成后", "After a model response completes"],
  SessionStart: ["会话开始时", "When a session starts"],
  SessionEnd: ["会话结束时", "When a session ends"],
  SubagentStart: ["子代理开始时", "When a subagent starts"],
  SubagentStop: ["子代理结束时", "When a subagent ends"],
  Notification: ["需要你关注时", "When your attention is needed"],
  PreCompact: ["压缩上下文前", "Before context is compacted"],
};

const events = kernel.match(/var Events = \[\]Event\{([^}]+)\}/)?.[1].match(/\b\w+\b/g) ?? [];

afterEach(() => {
  cleanup();
  localStorage.removeItem(STORAGE);
  boot();
});

it("covers every event declared by the kernel", () => {
  expect(events.length).toBeGreaterThan(0);
  expect(events).toEqual(Object.keys(LABELS));
});

for (const lang of ["zh", "en"] as const) {
  it(`renders ${lang} labels while saving canonical event ids`, async () => {
    localStorage.setItem(STORAGE, lang);
    boot();
    const catalog: HookCatalog = {
      globalPath: "/fixture/settings.json", projectPath: "", sources: [],
      events: [...events, "FutureEvent"].map((name) => ({ name, blocking: name === "PreToolUse", usesMatch: false })),
      hooks: [
        { event: "PreToolUse", command: "printf fixture", scope: "global" },
        { event: "PermissionRequest", command: "printf plugin", scope: "plugin", readOnly: true },
        { event: "FutureEvent", command: "printf future", scope: "plugin", readOnly: true },
      ],
    };
    const saveHooks = vi.fn(async () => {});
    const port = { hooks: vi.fn(async () => catalog), saveHooks } as unknown as AgentPort;
    const { container } = render(<Hooks port={port} onChanged={() => {}} />);
    await userEvent.click(await screen.findByRole("button", { name: lang === "zh" ? /手动添加/ : /Write one manually/ }));
    const select = screen.getByRole("combobox") as HTMLSelectElement;
    expect(select.value).toBe("PreToolUse");
    expect(Array.from(select.options, (option) => option.value)).toEqual([...events, "FutureEvent"]);
    for (const event of events) {
      expect(screen.getByRole("option", { name: `${LABELS[event][lang === "zh" ? 0 : 1]} (${event})` })).toBeTruthy();
    }
    expect(screen.getByRole("option", { name: "FutureEvent" })).toBeTruthy();
    expect(Array.from(container.querySelectorAll(".fromplugin .ev"), (el) => el.textContent)).toEqual([
      `${LABELS.PermissionRequest[lang === "zh" ? 0 : 1]} (PermissionRequest)`, "FutureEvent",
    ]);
    await userEvent.selectOptions(select, "Stop");
    await userEvent.click(screen.getByRole("button", { name: lang === "zh" ? "保存" : "Save" }));
    await waitFor(() => expect(saveHooks).toHaveBeenCalledWith("user", [expect.objectContaining({ event: "Stop" })]));
  });
}
