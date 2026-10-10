// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { act, cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { Settings } from "./Settings";
import { STORAGE, boot, t } from "../i18n";
import { MockPort } from "../port/mock";
import { MockHub } from "../port/mock_hub";
import type { GraphicsInfo } from "../port/host";
import type { SessionStatus } from "../port/port";

const answer: { now: GraphicsInfo | null } = { now: null };
vi.mock("../port/host", async (original) => {
  const real = await original<typeof import("../port/host")>();
  return { ...real, host: () => ({ ...real.host(), graphics: () => Promise.resolve(answer.now) }) };
});

afterEach(() => {
  cleanup();
  answer.now = null;
  localStorage.setItem(STORAGE, "zh");
  boot();
});

async function search(q: string) {
  render(<Settings
    hub={new MockHub() as never} port={new MockPort()}
    status={{ preset: "balanced", toolApprovalMode: "ask" } as SessionStatus}
    theme="light" onTheme={() => {}} contrast="" onContrast={() => {}} weight="" onWeight={() => {}}
    look={{} as never} onLook={() => {}} reloadThemes={() => {}}
    onClose={() => {}} onChanged={() => {}} onError={() => {}}
    account={null} accountUnread="" reloadAccount={() => {}}
  />);
  await act(async () => {});
  await userEvent.type(screen.getByRole("textbox", { name: t("搜索设置") }), q);
  return screen.queryAllByRole("option").some((o) => (o as HTMLElement).dataset.target === "graphics");
}

it("does not offer the graphics setting in a browser tab", async () => {
  expect(await search("gpu")).toBe(false);
});

it("offers the graphics setting where the shell answers", async () => {
  answer.now = { launchedOff: false, savedOff: false, compositing: "enabled" };
  expect(await search("gpu")).toBe(true);
});
