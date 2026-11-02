// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import "./testkit";
import { Settings } from "./Settings";
import type { SessionStatus } from "../port/port";
import { MockPort } from "../port/mock";
import { MockHub } from "../port/mock_hub";

afterEach(() => { cleanup(); vi.restoreAllMocks(); });

function draw(at: string) {
  return render(
    <Settings
      hub={new MockHub() as never}
      port={new MockPort() as never}
      status={{ preset: "balanced", toolApprovalMode: "ask" } as SessionStatus}
      theme="light" onTheme={() => {}} contrast="" onContrast={() => {}} weight="" onWeight={() => {}}
      look={{} as never} onLook={() => {}} reloadThemes={() => {}}
      onClose={() => {}} onChanged={() => {}} onError={() => {}}
      at={at as never}
      account={null} accountUnread="" reloadAccount={() => {}}
    />,
  );
}

it.each(["model", "tools", "appearance"])("%s opens on its first card, with no title block above it", async (section) => {
  const { container } = draw(section);
  await screen.findByRole("tablist", { name: "设置分类" });
  expect(container.querySelector(".prefs-intro")).toBeNull();
  expect(container.querySelector(".prefs-col")?.firstElementChild?.matches(".prefs-intro")).toBe(false);
  expect(screen.queryByText(/设置项：/)).toBeNull();
});
