// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import "./testkit";
import { Composer } from "./Composer";
import { Settings } from "./Settings";
import { MockPort } from "../port/mock";
import { MockHub } from "../port/mock_hub";
import type { AgentPort, ModelEntry, SessionStatus } from "../port/port";
import { boot, STORAGE, t } from "../i18n";

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  localStorage.setItem(STORAGE, "zh");
  boot();
});

const chat: ModelEntry[] = [
  { ref: "deepseek/deepseek-v4-pro", provider: "deepseek", vendor: "api.deepseek.com", model: "deepseek-v4-pro", kind: "openai", answers: "chat" },
  { ref: "deepseek/deepseek-v4-flash", provider: "deepseek", vendor: "api.deepseek.com", model: "deepseek-v4-flash", kind: "openai", answers: "chat" },
];
const decision: ModelEntry = { ref: "laya/typed-decisions", provider: "laya", vendor: "127.0.0.1", model: "typed-decisions", kind: "typesafe", answers: "decision" };

// The kernel's contract: a picker that names no scope gets the conversation models.
function kernelModels(port: AgentPort) {
  return vi.spyOn(port, "models").mockImplementation(async (answers) => {
    if (answers === "all") return [...chat, decision];
    return answers === "decision" ? [decision] : chat;
  });
}

it("the composer's model switcher never lists a decision source", async () => {
  const port = new MockPort() as unknown as AgentPort;
  const models = kernelModels(port);
  render(
    <Composer
      port={port} status={{ preset: "balanced", effort: "auto", toolApprovalMode: "ask", plan: false, modelRef: chat[0].ref } as SessionStatus}
      running={false} focus={0} onSubmit={vi.fn(async () => true)} onChanged={vi.fn()} onError={vi.fn()} git={null} draftKey="k"
    />,
  );
  fireEvent.click(document.querySelector('[data-action="model.select"]') as HTMLElement);
  await waitFor(() => expect(document.querySelectorAll(".studio-model-menu button.mi:not(.plain)").length).toBeGreaterThan(0));
  const rows = [...document.querySelectorAll(".studio-model-menu button.mi:not(.plain)")].map((n) => n.textContent ?? "");
  expect(rows.some((r) => r.includes("typed-decisions"))).toBe(false);
  expect(rows.filter((r) => r.includes("deepseek-v4"))).toHaveLength(2);
  expect(models.mock.calls.every(([answers]) => answers === undefined || answers === "chat")).toBe(true);
});

it("the settings table reads every source, then gives each job only what it can do", async () => {
  const port = new MockPort() as unknown as AgentPort;
  const models = kernelModels(port);
  vi.spyOn(port, "roles").mockResolvedValue({ web_search: "", planner: "", subagent: "", vision: "", guardian: "", decision: "" });
  render(
    <Settings
      hub={new MockHub() as never} port={port}
      status={{ preset: "balanced", toolApprovalMode: "ask", modelRef: chat[0].ref } as SessionStatus}
      theme="light" onTheme={() => {}} contrast="" onContrast={() => {}} weight="" onWeight={() => {}}
      look={{} as never} onLook={() => {}} reloadThemes={() => {}}
      onClose={() => {}} onChanged={() => {}} onError={() => {}} at="model"
      account={null} accountUnread="" reloadAccount={() => {}}
    />,
  );
  const main = (await screen.findByRole("combobox", { name: t("默认模型") })) as HTMLSelectElement;
  expect(models).toHaveBeenCalledWith("all");
  for (const name of ["默认模型", "计划", "子代理", "看图", "复核"]) {
    const options = within(screen.getByRole("combobox", { name: t(name) })).queryAllByRole("option").map((o) => o.textContent);
    expect(options.some((o) => o?.includes("typed-decisions")), name).toBe(false);
  }
  const decisionOptions = within(screen.getByRole("combobox", { name: t("决策") })).queryAllByRole("option").map((o) => o.textContent);
  expect(decisionOptions.filter((o) => o?.includes("deepseek"))).toHaveLength(0);
  expect(decisionOptions.some((o) => o?.includes("typed-decisions"))).toBe(true);
  expect(main.value).toBe(chat[0].ref);
});
