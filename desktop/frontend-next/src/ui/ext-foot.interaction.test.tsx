// @vitest-environment jsdom
import "./testkit";
import { afterEach, expect, it } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import { Settings } from "./Settings";
import { MockPort } from "../port/mock";
import { MockHub } from "../port/mock_hub";
import type { AgentPort, SessionStatus } from "../port/port";

afterEach(cleanup);

it("says in one line when extension changes take effect, with no card of its own for the runtime", async () => {
  const port = new MockPort() as unknown as AgentPort;
  render(<Settings
    hub={new MockHub() as never} port={port}
    status={{ preset: "balanced", toolApprovalMode: "ask" } as SessionStatus}
    theme="light" onTheme={() => {}} contrast="" onContrast={() => {}} weight="" onWeight={() => {}}
    look={{} as never} onLook={() => {}} reloadThemes={() => {}}
    onClose={() => {}} onChanged={() => {}} onError={() => {}} at="ext:installed"
    account={null} accountUnread="" reloadAccount={() => {}}
  />);
  const foot = await screen.findByRole("note");
  expect(foot.textContent).toContain("从下一轮开始生效");
  expect(foot.querySelector('[data-action="extensions.reload"]')).toBeTruthy();
  expect(document.getElementById("set-ext-runtime")).toBeNull();
  expect([...document.querySelectorAll(".grp-hd h3")].map((h) => h.textContent)).not.toContain("运行时");
});
