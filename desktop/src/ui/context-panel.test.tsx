// @vitest-environment jsdom

import { invoke } from "@tauri-apps/api/core";
import { openPath } from "@tauri-apps/plugin-opener";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Settings, UsageStats } from "../App";
import { setLang } from "../i18n";
import { ContextPanel } from "./context-panel";

vi.mock("@tauri-apps/api/core", () => ({ invoke: vi.fn() }));
vi.mock("@tauri-apps/plugin-opener", () => ({ openPath: vi.fn() }));

const usage: UsageStats = {
  totalCostUsd: 0,
  totalPromptTokens: 0,
  totalCompletionTokens: 0,
  cacheHitTokens: 0,
  cacheMissTokens: 0,
  lastCallCacheHit: null,
  lastCallCacheMiss: null,
  reservedTokens: 0,
};

const settings: Settings = {
  reasoningEffort: "high",
  editMode: "review",
  budgetUsd: null,
  workspaceDir: "/repo",
  recentWorkspaces: [],
  model: "deepseek-reasoner",
  preset: "pro",
  version: "0.0.0",
};

function renderPanel() {
  return render(
    <ContextPanel
      settings={settings}
      usage={usage}
      mcpSpecs={[]}
      mcpBridged={false}
      sessionFiles={[{ path: "src/new-file.ts", status: "m" }]}
      memory={[]}
    />,
  );
}

describe("ContextPanel files", () => {
  beforeEach(() => {
    setLang("en");
    vi.mocked(invoke).mockReset();
    vi.mocked(openPath).mockReset();
  });
  afterEach(cleanup);

  it("keeps each tracked file's full path visible", () => {
    const { container } = renderPanel();

    const fileRow = container.querySelector('[data-kind="file"]');

    expect(fileRow?.textContent).toContain("src/new-file.ts");
    expect(fileRow?.getAttribute("title")).toBe("src/new-file.ts");
  });

  it("opens a tracked file from the file row action", async () => {
    renderPanel();

    fireEvent.click(screen.getByRole("button", { name: "Open file: src/new-file.ts" }));

    await waitFor(() => expect(openPath).toHaveBeenCalledWith("/repo/src/new-file.ts"));
  });
});
