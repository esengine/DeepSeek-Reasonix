// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { act, cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { Settings } from "./Settings";
import { MockPort } from "../port/mock";
import { MockHub } from "../port/mock_hub";
import type { CapabilityScope, SkillCatalog } from "../port/port";

afterEach(cleanup);

it("keeps the selected project's skills when the previous read finishes late", async () => {
  const port = new MockPort();
  const scopes: CapabilityScope[] = [
    { root: "/projects/a", name: "Project A", key: "a", repo: true, overrides: 0, current: true },
    { root: "/projects/b", name: "Project B", key: "b", repo: true, overrides: 0 },
  ];
  port.capabilityScopes = vi.fn(async () => scopes);
  port.mcp = vi.fn(async (root?: string) => ({ servers: [], scope: root === scopes[1].root ? scopes[1] : scopes[0] }));
  let finishA!: (value: SkillCatalog) => void;
  port.skills = vi.fn((root?: string) => root === scopes[1].root
    ? Promise.resolve({ implicit: true, skills: [{ name: "only-b", enabled: true }] })
    : new Promise<SkillCatalog>((resolve) => { finishA = resolve; }));
  render(<Settings
    hub={new MockHub()} onError={() => {}} port={port} status={null} theme="" reloadThemes={() => {}}
    onTheme={() => {}} contrast="" onContrast={() => {}} weight="" onWeight={() => {}}
    look={{}} onLook={() => {}} onClose={() => {}} onChanged={() => {}} at="ext" account={null}
    accountUnread="" reloadAccount={() => {}}
  />);

  await userEvent.click(await screen.findByRole("button", { name: /Project A/ }));
  await userEvent.click(screen.getByRole("option", { name: /Project B/ }));
  await screen.findByText("only-b");
  await act(async () => finishA({ implicit: true, skills: [{ name: "only-a", enabled: true }] }));
  expect(screen.getByText("only-b")).toBeTruthy();
  expect(screen.queryByText("only-a")).toBeNull();
});
