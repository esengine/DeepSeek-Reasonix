// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { Rules } from "./Rules";
import { MockPort } from "../port/mock";
import type { AgentPort, PermissionRules } from "../port/port";

afterEach(cleanup);

const rules = (granted: string[]): PermissionRules => ({
  mode: "ask", deny: [], ask: [], allow: [], path: "/home/u/.reasonix/config.toml", granted,
});

function draw(granted: string[], revoke = vi.fn(async () => rules([]))) {
  const port = {
    ...(new MockPort() as unknown as AgentPort),
    permissions: async () => rules(granted),
    revokeSessionGrant: revoke,
  } as unknown as AgentPort;
  render(<Rules port={port} onChanged={vi.fn()} />);
  return { revoke };
}

// "Allow for this session" is written down nowhere, so the file on screen is
// less than the agent may currently do — and before this there was no way back
// from one except ending the session.
describe("what a prompt allowed for this session", () => {
  it("lists the grants no file carries, and says they are this session only", async () => {
    draw(["Computer=com.example.Notes", "Browser=https://example.com"]);
    await screen.findByText("Computer=com.example.Notes");
    expect(screen.getByText("Browser=https://example.com")).toBeTruthy();
    expect(screen.getByText("本次会话另外允许了 2 项")).toBeTruthy();
    expect(screen.getByText("只在这个会话里有效，没有写进文件")).toBeTruthy();
  });

  it("takes one back by name, and all of them by none", async () => {
    const { revoke } = draw(["Computer=com.example.Notes"]);
    await screen.findByText("Computer=com.example.Notes");
    await userEvent.click(screen.getByLabelText("收回 Computer=com.example.Notes"));
    await waitFor(() => expect(revoke).toHaveBeenCalledWith("Computer=com.example.Notes"));

    cleanup();
    const all = draw(["Computer=com.example.Notes", "Browser=https://example.com"]);
    await screen.findByText("全部收回");
    await userEvent.click(screen.getByText("全部收回"));
    await waitFor(() => expect(all.revoke).toHaveBeenCalledWith(""));
  });

  it("says nothing where a session was given nothing", async () => {
    const { container } = render(
      <Rules port={{ ...(new MockPort() as unknown as AgentPort), permissions: async () => rules([]) } as unknown as AgentPort} onChanged={vi.fn()} />,
    );
    await waitFor(() => expect(container.querySelector(".rhead")).toBeTruthy());
    expect(container.querySelector(".granted")).toBeNull();
  });
});

it("revokes a remembered project rule without asking the user to edit a fingerprinted file", async () => {
  const rememberedPath = "/home/u/.reasonix/project-grants.json";
  const revokeRememberedProjectRule = vi.fn(async () => ({ ...rules([]), rememberedPath }));
  const changed = vi.fn();
  const port = {
    ...(new MockPort() as unknown as AgentPort),
    permissions: async () => ({ ...rules([]), remembered: ["Bash(go test:*)"], rememberedPath }),
    revokeRememberedProjectRule,
  } as unknown as AgentPort;
  render(<Rules port={port} onChanged={changed} />);
  expect(await screen.findByText("Bash(go test:*)")).toBeTruthy();
  expect(screen.getByText(rememberedPath)).toBeTruthy();
  await userEvent.click(screen.getByLabelText("收回 Bash(go test:*)"));
  await waitFor(() => expect(revokeRememberedProjectRule).toHaveBeenCalledWith("Bash(go test:*)"));
  expect(changed).toHaveBeenCalledOnce();
  expect(screen.queryByText("Bash(go test:*)")).toBeNull();
});

it("shows a malformed remembered-rules file instead of silently hiding the failure", async () => {
  const port = {
    ...(new MockPort() as unknown as AgentPort),
    permissions: async () => ({ ...rules([]), rememberedPath: "/home/u/.reasonix/project-grants.json", rememberedErrorCode: "project_grants.unavailable" }),
  } as unknown as AgentPort;
  render(<Rules port={port} onChanged={vi.fn()} />);
  expect(await screen.findByText("已记住的权限规则无法读取")).toBeTruthy();
  expect(screen.getByText("请检查或修复用户目录中的授权记录文件。")).toBeTruthy();
  expect(screen.getByText("/home/u/.reasonix/project-grants.json")).toBeTruthy();
  expect(screen.queryByText(/invalid JSON/)).toBeNull();
});

describe("saved rules that name no tool", () => {
  const dormantRules = (): PermissionRules => ({
    ...rules([]), ask: ["rm", "bash(git push:*)"], deny: ["git reset"],
    dormant: [{ list: "deny", rule: "git reset", tool: "git reset" }, { list: "ask", rule: "rm", tool: "rm" }],
  });

  it("says they never take effect, and marks only those rows", async () => {
    const port = { ...(new MockPort() as unknown as AgentPort), permissions: async () => dormantRules() } as unknown as AgentPort;
    const { container } = render(<Rules port={port} onChanged={vi.fn()} />);
    expect(await screen.findByText("有 2 条规则没有对应的工具，不会生效")).toBeTruthy();
    const marked = [...container.querySelectorAll(".rgroup")].map((g) => [g.querySelector(".rg-hd code")?.textContent, g.querySelectorAll("[data-dormant]").length]);
    expect(marked).toContainEqual(["rm", 1]);
    expect(marked).toContainEqual(["git reset", 1]);
    expect(marked).toContainEqual(["bash", 0]);
    const how = [...container.querySelectorAll(".rgroup")].map((g) => [g.querySelector(".rg-hd code")?.textContent, !!g.querySelector(".rg-hd .how")]);
    expect(how).toContainEqual(["rm", false]);
    expect(how).toContainEqual(["bash", true]);
  });

  it("says nothing when every rule names a tool", async () => {
    const port = { ...(new MockPort() as unknown as AgentPort), permissions: async () => rules([]) } as unknown as AgentPort;
    const { container } = render(<Rules port={port} onChanged={vi.fn()} />);
    await waitFor(() => expect(container.querySelector(".rhead")).toBeTruthy());
    expect(container.querySelector("[data-dormant]")).toBeNull();
    expect(screen.queryByText(/没有对应的工具/)).toBeNull();
  });
});
