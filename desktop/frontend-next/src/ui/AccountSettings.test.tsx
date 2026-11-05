// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import "./testkit";
import { MockPort } from "../port/mock";
import type { AccountState, AgentPort } from "../port/port";
import { AccountSettings } from "./AccountSettings";
import { SettingDeepContext } from "./SettingDeep";

afterEach(cleanup);

function draw(state: AccountState | null) {
  const go = vi.fn();
  const port = new MockPort() as unknown as AgentPort;
  render(
    <SettingDeepContext.Provider value={{ pages: true, open: null, section: "账号与关于", go }}>
      <AccountSettings port={port} homePort={port} state={state} unread="" reload={() => {}} />
    </SettingDeepContext.Provider>,
  );
  return go;
}

it("is account, version and diagnostics, and about: one row per setting", () => {
  draw({ signedIn: false });
  expect([...document.querySelectorAll(".prefs-card > header h3")].map((h) => h.textContent)).toEqual(["账号", "版本与诊断", "关于"]);
  for (const id of ["account", "versions", "logs", "community", "sponsor"]) expect(document.querySelector(`[data-setting="${id}"]`), id).toBeTruthy();
});

it("offers sign-in when signed out and the account when signed in", () => {
  const go = draw({ signedIn: false });
  fireEvent.click(screen.getByRole("button", { name: "登录" }));
  expect(go).toHaveBeenLastCalledWith("account");
  cleanup();
  draw({ signedIn: true, user: { handle: "demo", email: "d@example.com", label: "demo" } });
  expect(screen.getByRole("button", { name: "账号信息" })).toBeTruthy();
});

it("opens versions and logs from their pills", () => {
  const go = draw({ signedIn: false });
  fireEvent.click(screen.getByRole("button", { name: "版本与更新" }));
  expect(go).toHaveBeenLastCalledWith("versions");
});
