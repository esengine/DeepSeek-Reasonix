// @vitest-environment jsdom
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";

// These tests describe the feedback channel while it exists; the shipped build has it retired.
vi.mock("../feedbackchannel", () => ({ FEEDBACK_RETIRED: false }));
import "./testkit";
import { MockPort } from "../port/mock";
import type { AgentPort } from "../port/port";
import { Appearance } from "./Appearance";
import { ChatDisplaySettings } from "./ChatDisplaySettings";
import { OpsSettings } from "./OpsSettings";

afterEach(cleanup);

const frame = (node: React.ReactNode) =>
  render(<>{node}</>);
const titles = () => [...document.querySelectorAll(".prefs-card > header h3")].map((h) => h.textContent);
const has = (id: string) => !!document.querySelector(`[data-setting="${id}"]`);

it("expands appearance editors in theme, text and window cards", async () => {
  const port = new MockPort() as unknown as AgentPort;
  frame(<Appearance port={port} theme="auto" onTheme={() => {}} contrast="" onContrast={() => {}} weight="" onWeight={() => {}}
    reloadThemes={() => {}} look={{}} onLook={() => {}} />);
  expect(titles().slice(0, 3)).toEqual(["主题", "文字", "窗口"]);
  for (const id of ["mode", "scheme", "wallpaper", "effects", "font", "size", "weight", "contrast", "window"]) await waitFor(() => expect(has(id), id).toBe(true));
  expect(has("advanced-appearance")).toBe(false);
  expect(screen.getByRole("combobox", { name: "界面" })).toBeTruthy();
  expect(screen.getByRole("slider", { name: "界面大小微调" })).toBeTruthy();
});

it("conversation display is process, files and previews, and notices", async () => {
  const port = new MockPort() as unknown as AgentPort;
  frame(<ChatDisplaySettings port={port} />);
  expect(titles()).toEqual(["过程", "文件与预览", "提示"]);
  for (const id of ["fold-work", "folding", "plan-progress", "changed-files", "present-files", "office-preview", "new-answer-marker", "feedback-level-notices"]) {
    await waitFor(() => expect(has(id), id).toBe(true));
  }
});

it("operation and performance is shortcuts, tabs and navigation, while running, and performance", () => {
  frame(<OpsSettings />);
  expect(titles()).toEqual(["快捷键", "标签与导航", "运行中", "性能"]);
  for (const id of ["keys", "esc-stop", "session-tabs", "session-quick-actions", "navrail", "naventries", "live-grace", "running-message-intent", "composer-hint", "page-sleep"]) {
    expect(has(id), id).toBe(true);
  }
});
