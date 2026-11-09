// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { Settings } from "./Settings";
import { MockPort } from "../port/mock";
import { MockHub } from "../port/mock_hub";
import type { AgentPort, SessionStatus } from "../port/port";

afterEach(cleanup);

function draw(onClose = vi.fn()) {
  render(<Settings
    hub={new MockHub() as never} port={new MockPort() as unknown as AgentPort}
    status={{ preset: "balanced", toolApprovalMode: "ask" } as SessionStatus}
    theme="light" onTheme={() => {}} contrast="" onContrast={() => {}} weight="" onWeight={() => {}}
    look={{} as never} onLook={() => {}} reloadThemes={() => {}}
    onClose={onClose} onChanged={vi.fn()} onError={() => {}} at="appearance"
    account={null} accountUnread="" reloadAccount={() => {}}
  />);
  return onClose;
}

it("returns to the workspace through the labelled full-screen header", async () => {
  const onClose = draw();
  const back = screen.getByRole("button", { name: "关闭 Esc" });
  expect(back.textContent).toContain("返回工作区");
  expect(back.closest(".prefs-windowbar")).toBeTruthy();
  await userEvent.click(back);
  await vi.waitFor(() => expect(onClose).toHaveBeenCalledExactlyOnceWith());
});

it("owns a full-screen surface and a title bar instead of an outer veil", () => {
  draw();
  expect(document.querySelector(".prefs.prefs-screen > .prefs-windowbar")).toBeTruthy();
  expect(document.querySelector(".prefs-strip")).toBeNull();
});
