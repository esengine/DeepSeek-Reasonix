// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import "./testkit";
import { Settings } from "./Settings";
import { MockPort } from "../port/mock";
import { MockHub } from "../port/mock_hub";

afterEach(() => { cleanup(); vi.restoreAllMocks(); history.replaceState(null, "", "/"); });

const drawn = () => render(<Settings hub={new MockHub()} onError={() => {}} port={new MockPort()} networkPort={new MockPort()}
  status={null} theme="" reloadThemes={() => {}} onTheme={() => {}} contrast="" onContrast={() => {}}
  weight="" onWeight={() => {}} look={{}} onLook={() => {}} onClose={() => {}} onChanged={() => {}}
  at="remote:phone" account={null} accountUnread="" reloadAccount={() => {}} />);

it("offers a phone its own reminders only on a page that controls a computer", async () => {
  history.replaceState(null, "", "/?device=" + "ab".repeat(32));
  drawn();
  expect(await screen.findByRole("heading", { name: "这台手机的提醒" })).toBeTruthy();
});

it("leaves the desktop's own window without a phone's reminder block", async () => {
  drawn();
  await screen.findByRole("heading", { name: "手机访问" });
  expect(screen.queryByRole("heading", { name: "这台手机的提醒" })).toBeNull();
});
