// @vitest-environment jsdom
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it } from "vitest";
import "./testkit";
import { MockPort } from "../port/mock";
import type { AccountState, AgentPort } from "../port/port";
import { AccountSettings } from "./AccountSettings";

afterEach(cleanup);

function draw(state: AccountState | null) {
  const port = new MockPort() as unknown as AgentPort;
  render(
    <>
      <AccountSettings port={port} homePort={port} state={state} unread="" reload={() => {}} />
    </>,
  );
}

it("is account, version and diagnostics, and about: one row per setting", () => {
  draw({ signedIn: false });
  expect([...document.querySelectorAll(".prefs-card > header h3")].map((h) => h.textContent)).toEqual(["账号", "版本与诊断", "关于"]);
  for (const id of ["account", "versions", "logs", "community", "sponsor"]) expect(document.querySelector(`[data-setting="${id}"]`), id).toBeTruthy();
});

it("offers sign-in when signed out and the account when signed in", () => {
  draw({ signedIn: false });
  expect(document.querySelector('[data-action="account.sign-in"]')).toBeTruthy();
  cleanup();
  draw({ signedIn: true, user: { handle: "demo", email: "d@example.com", label: "demo" } });
  expect(screen.getByText("d@example.com")).toBeTruthy();
  expect(document.querySelector('[data-action="account.sign-out"]')).toBeTruthy();
});

it("shows versions and diagnostics inline", async () => {
  draw({ signedIn: false });
  await waitFor(() => expect(document.querySelector("#set-versions .vnow")).toBeTruthy());
  expect(document.querySelector('#set-logs .grp-items')).toBeTruthy();
});
