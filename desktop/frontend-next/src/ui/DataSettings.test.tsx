// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import "./testkit";
import { MockHub } from "../port/mock_hub";
import { MockPort } from "../port/mock";
import type { AgentPort } from "../port/port";
import { DataSettings } from "./DataSettings";
import { SettingDeepContext } from "./SettingDeep";

afterEach(cleanup);

function draw(signedIn = false) {
  const go = vi.fn();
  const port = new MockPort() as unknown as AgentPort;
  render(
    <SettingDeepContext.Provider value={{ pages: true, open: null, section: "数据", go }}>
      <DataSettings port={port} homePort={port} localPort={port} hub={new MockHub()} workspace="/w" cwd="/w" busy="" signedIn={signedIn}
        memories={7} onIsolate={() => {}} onChanged={() => {}} onRecovered={() => {}} />
    </SettingDeepContext.Provider>,
  );
  return go;
}

it("is three cards: memory and usage, session storage, backup, each setting one row", async () => {
  draw();
  expect([...document.querySelectorAll(".prefs-card > header h3")].map((h) => h.textContent)).toEqual(["记忆与用量", "会话存储", "备份"]);
  for (const id of ["memory", "usage", "upload-usage", "currency", "storage", "auto-archive", "session-dir", "legacy-sessions", "backup-file"]) {
    await waitFor(() => expect(document.querySelector(`[data-setting="${id}"]`), id).toBeTruthy());
  }
  expect(document.querySelector('[data-setting="backup"]')).toBeNull();
});

it("opens the memory, usage and storage pages from pills, with the memory count", () => {
  const go = draw();
  for (const [name, id] of [["打开记忆（7）", "memory"], ["查看用量", "usage"], ["管理", "storage"], ["导出与导入…", "backup-file"]] as const) {
    fireEvent.click(screen.getByRole("button", { name }));
    expect(go).toHaveBeenLastCalledWith(id);
  }
});

it("offers the cloud backup only when signed in", () => {
  draw(true);
  expect(document.querySelector('[data-setting="backup"]')).toBeTruthy();
});
