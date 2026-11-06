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

it("has one control in its corner, the one that closes it", async () => {
  const onClose = draw();
  const buttons = document.querySelectorAll(".prefs-actions button");
  expect(buttons).toHaveLength(1);
  expect(screen.queryByRole("button", { name: "放大设置窗口" })).toBeNull();
  expect(document.querySelector(".prefs-sheet")?.hasAttribute("data-wide")).toBe(false);
  await userEvent.click(buttons[0]!);
  await vi.waitFor(() => expect(onClose).toHaveBeenCalled());
});

it("blurs the title bar behind it too, without taking its clicks", () => {
  draw();
  const strip = document.querySelector(".prefs-strip");
  expect(strip?.getAttribute("aria-hidden")).toBe("true");
  expect(strip?.nextElementSibling?.classList.contains("prefs")).toBe(true);
});
