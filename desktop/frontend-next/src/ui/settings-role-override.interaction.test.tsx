// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { Settings } from "./Settings";
import { HttpError, type AgentPort, type SessionStatus } from "../port/port";
import { MockPort } from "../port/mock";
import { MockHub } from "../port/mock_hub";

afterEach(() => { cleanup(); vi.restoreAllMocks(); });

function draw(port: AgentPort) {
  render(
    <Settings
      hub={new MockHub() as never}
      port={port}
      status={{ preset: "balanced", toolApprovalMode: "ask" } as SessionStatus}
      theme="light" onTheme={() => {}} contrast="" onContrast={() => {}} weight="" onWeight={() => {}}
      look={{} as never} onLook={() => {}} reloadThemes={() => {}}
      onClose={() => {}} onChanged={() => {}} onError={() => {}}
      at="model"
      account={null} accountUnread="" reloadAccount={() => {}}
    />,
  );
}

function withOverride(scope: "user" | "project" = "user") {
  const port = new MockPort();
  port.overrides = { subagent: [{ key: "task", model: "deepseek/deepseek-pro", scope }] };
  return port as unknown as AgentPort;
}

it("says a subagent_models entry outranks the subagent row instead of leaving the row looking inert", async () => {
  draw(withOverride());
  const note = await screen.findByText(/「task」已被配置固定为 deepseek\/deepseek-pro/);
  expect(note.closest("[role=row]")?.textContent).toContain("子代理");
});

it("shows nothing extra while no entry outranks the row", async () => {
  draw(new MockPort() as unknown as AgentPort);
  await screen.findByRole("combobox", { name: "子代理" });
  expect(screen.queryByText(/已被配置固定为/)).toBeNull();
});

it("clearing the entry asks the kernel, then reads the overrides again so the note goes", async () => {
  const port = withOverride();
  const clear = vi.spyOn(port, "clearRoleOverride");
  draw(port);
  await userEvent.click(await screen.findByRole("button", { name: "改回跟随这里" }));
  expect(clear).toHaveBeenCalledWith("subagent", "task");
  await waitFor(() => expect(screen.queryByText(/已被配置固定为/)).toBeNull());
});

it("a project-scoped entry is named but offers no clear the kernel would refuse", async () => {
  draw(withOverride("project"));
  await screen.findByText(/已被配置固定为/);
  expect(screen.queryByRole("button", { name: "改回跟随这里" })).toBeNull();
  expect(screen.getByText("来自项目配置，需在项目里修改")).toBeTruthy();
});

it("a refused clear is reported by code and the note stays", async () => {
  const port = withOverride();
  vi.spyOn(port, "clearRoleOverride").mockRejectedValue(new HttpError(409, "x", { code: "roles.override_not_in_user_config", error: "x", params: { key: "task" } }));
  draw(port);
  await userEvent.click(await screen.findByRole("button", { name: "改回跟随这里" }));
  expect(await screen.findByText(/「task」不在用户配置里/)).toBeTruthy();
  expect(screen.getByText(/已被配置固定为/)).toBeTruthy();
});

it("choosing a subagent model leaves the override note in place: the kernel still reports the entry", async () => {
  const port = withOverride();
  draw(port);
  const select = (await screen.findByRole("combobox", { name: "子代理" })) as HTMLSelectElement;
  const other = Array.from(select.options).find((o) => o.value && o.value !== select.value)!.value;
  await userEvent.selectOptions(select, other);
  await waitFor(() => expect((screen.getByRole("combobox", { name: "子代理" }) as HTMLSelectElement).value).toBe(other));
  expect(screen.getByText(/已被配置固定为/)).toBeTruthy();
});
