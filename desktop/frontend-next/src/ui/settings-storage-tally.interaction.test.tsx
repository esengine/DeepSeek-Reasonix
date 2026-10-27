// @vitest-environment jsdom
import { act, cleanup, render } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import "./testkit";
import { Settings } from "./Settings";
import type { SessionStatus } from "../port/port";
import { MockPort } from "../port/mock";
import { MockHub } from "../port/mock_hub";

beforeEach(() => { vi.useFakeTimers(); });
afterEach(() => { cleanup(); vi.useRealTimers(); });

const draw = (port: MockPort) => render(
  <Settings
    hub={new MockHub() as never}
    port={port}
    status={{ preset: "balanced", toolApprovalMode: "ask" } as SessionStatus}
    theme="light" onTheme={() => {}} contrast="" onContrast={() => {}} weight="" onWeight={() => {}}
    look={{} as never} onLook={() => {}} reloadThemes={() => {}}
    onClose={vi.fn()} onChanged={() => {}} onError={() => {}}
    account={null} accountUnread="" reloadAccount={() => {}}
  />,
);

it("does not walk the storage roots while the settings window is opening", async () => {
  const port = new MockPort();
  const walk = vi.spyOn(port, "storage");
  draw(port);
  await act(async () => { await vi.advanceTimersByTimeAsync(200); });
  expect(walk).not.toHaveBeenCalled();
  await act(async () => { await vi.advanceTimersByTimeAsync(3000); });
  expect(walk).toHaveBeenCalledTimes(1);
});

it("does not walk them again when the window is reopened soon after", async () => {
  const port = new MockPort();
  const walk = vi.spyOn(port, "storage");
  const first = draw(port);
  await act(async () => { await vi.advanceTimersByTimeAsync(3000); });
  first.unmount();
  draw(port);
  await act(async () => { await vi.advanceTimersByTimeAsync(3000); });
  expect(walk).toHaveBeenCalledTimes(1);
});
