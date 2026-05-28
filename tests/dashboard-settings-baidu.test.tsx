// @vitest-environment jsdom

import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import React from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { Settings } from "../dashboard/src/App";
import type { SettingsPatch } from "../dashboard/src/protocol";
import { SettingsModal } from "../dashboard/src/ui/settings";

vi.mock("@tauri-apps/plugin-opener", () => ({
  openUrl: vi.fn(),
}));

afterEach(() => {
  cleanup();
});

const BASE_SETTINGS: Settings = {
  reasoningEffort: "high",
  editMode: "review",
  budgetUsd: null,
  baseUrl: "",
  apiKeyPrefix: undefined,
  workspaceDir: "/tmp/reasonix",
  recentWorkspaces: [],
  model: "deepseek-v4-flash",
  editor: "code",
  webSearchEngine: "baidu",
  version: "test",
};

function renderSettingsModal(
  settings: Settings = BASE_SETTINGS,
  onSave: (patch: SettingsPatch) => void = vi.fn(),
) {
  render(
    <SettingsModal
      settings={settings}
      balance={null}
      usage={{
        totalCostUsd: 0,
        totalPromptTokens: 0,
        totalCompletionTokens: 0,
        cacheHitTokens: 0,
        cacheMissTokens: 0,
        lastCallCacheHit: null,
        lastCallCacheMiss: null,
        reservedTokens: 0,
      }}
      currency="CNY"
      theme="auto"
      themeStyle="default"
      onSetTheme={vi.fn()}
      onSetThemeStyle={vi.fn()}
      fontScale="md"
      onSetFontScale={vi.fn()}
      fontFamily="sans"
      onSetFontFamily={vi.fn()}
      initialPage="general"
      mcpSpecs={[]}
      mcpBridged={false}
      skills={[]}
      memory={[]}
      memoryDetail={null}
      qq={null}
      onClose={vi.fn()}
      onSave={onSave}
      onSaveApiKey={vi.fn()}
      onLoadQQ={vi.fn()}
      onConnectQQ={vi.fn()}
      onDisconnectQQ={vi.fn()}
      onSaveQQConfig={vi.fn()}
      onOpenQQApplyLink={vi.fn()}
      onPickWorkspace={vi.fn()}
      onAddMcpSpec={vi.fn()}
      onRemoveMcpSpec={vi.fn()}
      onReadMemory={vi.fn()}
    />,
  );
}

describe("dashboard settings Baidu web search", () => {
  it("renders Baidu as a selectable search engine", () => {
    const onSave = vi.fn();
    renderSettingsModal({ ...BASE_SETTINGS, webSearchEngine: "bing" }, onSave);

    const select = screen.getByDisplayValue(/bing/);
    fireEvent.change(select, { target: { value: "baidu" } });

    expect(onSave).toHaveBeenCalledWith({ webSearchEngine: "baidu" });
  });

  it("saves the Baidu API key from the dashboard settings page", () => {
    const onSave = vi.fn();
    renderSettingsModal(
      {
        ...BASE_SETTINGS,
        webSearchApiKeys: {
          baidu: "bce-v3…abc",
        },
      },
      onSave,
    );

    expect(screen.getByText(/Baidu API key/)).toBeTruthy();

    const input = screen.getByPlaceholderText("bce-v3…abc");
    fireEvent.change(input, { target: { value: "  baidu-test-key  " } });
    fireEvent.click(screen.getAllByRole("button", { name: "Save key" })[0]!);

    expect(onSave).toHaveBeenCalledWith({ baiduApiKey: "baidu-test-key" });
  });
});
