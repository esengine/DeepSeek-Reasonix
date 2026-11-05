// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import "./testkit";
import { ModelSettings } from "./ModelSettings";
import { SettingDeepContext } from "./SettingDeep";
import type { AgentPort } from "../port/port";

afterEach(cleanup);

const entry = (name: string, baseUrl: string) => ({ name, baseUrl, kind: "openai", keyEnv: name.toUpperCase() });

function draw(go = vi.fn()) {
  const port = {
    providers: async () => [entry("a", "https://api.one.test/v1"), entry("b", "https://api.two.test/v1")],
    compaction: async () => ({ context_window: 128000, ratio: 0.9, trigger: 115200 }),
    providerHealthSettings: async () => ({ enabled: false }),
    saveProviderHealthSettings: async (enabled: boolean) => ({ enabled }),
  } as unknown as AgentPort;
  render(
    <SettingDeepContext.Provider value={{ pages: true, open: null, section: "模型", go }}>
      <ModelSettings port={port} onChanged={() => {}} onFailed={() => {}}
        usage={{} as never} providers={{} as never} />
    </SettingDeepContext.Provider>,
  );
  return go;
}

it("is two cards of four rows: services and health, uses and context", async () => {
  draw();
  expect(screen.getByRole("heading", { name: "服务" })).toBeTruthy();
  expect(screen.getByRole("heading", { name: "用途与上下文" })).toBeTruthy();
  expect(document.querySelectorAll(".prefs-card > .srow, .prefs-card > [data-setting]")).toHaveLength(4);
  await waitFor(() => expect(screen.getByText("已接入 2 个服务；地址、密钥、模型列表在这里改")).toBeTruthy());
  expect(screen.getByText("上下文到多少开始压缩；默认窗口的 90%")).toBeTruthy();
});

it("opens each page from its row's pill", async () => {
  const go = draw();
  await waitFor(() => expect(screen.getByRole("button", { name: "90%" })).toBeTruthy());
  for (const [name, id] of [["管理服务", "providers"], ["按用途设置", "model"], ["90%", "context"]] as const) {
    fireEvent.click(screen.getByRole("button", { name }));
    expect(go).toHaveBeenLastCalledWith(id);
  }
});
