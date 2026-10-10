// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { Settings } from "./Settings";
import { HttpError, type AgentPort, type SessionStatus } from "../port/port";
import { MockPort } from "../port/mock";
import { MockHub } from "../port/mock_hub";

afterEach(() => { cleanup(); vi.restoreAllMocks(); });

function draw(port: AgentPort, networkPort?: AgentPort) {
  render(
    <Settings
      hub={new MockHub() as never}
      port={port}
      networkPort={networkPort}
      status={{ preset: "balanced", toolApprovalMode: "ask" } as SessionStatus}
      theme="light" onTheme={() => {}} contrast="" onContrast={() => {}} weight="" onWeight={() => {}}
      look={{} as never} onLook={() => {}} reloadThemes={() => {}}
      onClose={() => {}} onChanged={() => {}} onError={() => {}}
      at="model"
      account={null} accountUnread="" reloadAccount={() => {}}
    />,
  );
}

// The row's value is the row the picker is about to overwrite, so the pick has
// to be readable back: `other` is never the one already selected.
async function pickAnother(select: HTMLSelectElement) {
  const other = Array.from(select.options).find((o) => o.value && o.value !== select.value);
  expect(other).toBeTruthy();
  await userEvent.selectOptions(select, other!.value);
  return other!.value;
}

it("the settings default-model row records the pick as the default, not just for this session", async () => {
  const port = new MockPort() as unknown as AgentPort;
  const set = vi.spyOn(port, "setModel");
  const setDefault = vi.spyOn(port, "setDefaultModel");
  draw(port);
  const select = (await screen.findByRole("combobox", { name: "默认模型" })) as HTMLSelectElement;
  const picked = await pickAnother(select);
  expect(set).toHaveBeenCalledWith(picked);
  expect(setDefault).toHaveBeenCalledWith(picked);
});

// A remote pane's models come from this machine's broker, so the default the
// row describes is this machine's: the pick is written there and the pane's own
// kernel, which has no default_model it reads, is only asked to switch.
it("in a remote pane the pick switches that pane and is recorded on this machine", async () => {
  const remote = new MockPort() as unknown as AgentPort;
  const local = new MockPort() as unknown as AgentPort;
  const switched = vi.spyOn(remote, "setModel");
  const remoteDefault = vi.spyOn(remote, "setDefaultModel");
  const localDefault = vi.spyOn(local, "setDefaultModel");
  const localSwitched = vi.spyOn(local, "setModel");
  draw(remote, local);
  const select = (await screen.findByRole("combobox", { name: "默认模型" })) as HTMLSelectElement;
  const picked = await pickAnother(select);
  expect(switched).toHaveBeenCalledWith(picked);
  expect(localDefault).toHaveBeenCalledWith(picked);
  expect(remoteDefault).not.toHaveBeenCalled();
  expect(localSwitched).not.toHaveBeenCalled();
});

it("in a remote pane the row follows this machine's default, not the pane's lagging catalogue", async () => {
  const remote = new MockPort() as unknown as AgentPort;
  const local = new MockPort() as unknown as AgentPort;
  vi.spyOn(remote, "setDefaultModel").mockResolvedValue();
  draw(remote, local);
  const select = (await screen.findByRole("combobox", { name: "默认模型" })) as HTMLSelectElement;
  const picked = await pickAnother(select);
  const after = (await screen.findByRole("combobox", { name: "默认模型" })) as HTMLSelectElement;
  expect(after.value).toBe(picked);
});

it("a refused default is shown and the row keeps the previous default", async () => {
  const port = new MockPort() as unknown as AgentPort;
  vi.spyOn(port, "setDefaultModel").mockRejectedValue(new HttpError(409, "x", { code: "settings.default_model_brokered", error: "x" }));
  draw(port);
  const select = (await screen.findByRole("combobox", { name: "默认模型" })) as HTMLSelectElement;
  const before = select.value;
  await pickAnother(select);
  expect(await screen.findByText(/由模型所在的那台机器决定/)).toBeTruthy();
  expect(((await screen.findByRole("combobox", { name: "默认模型" })) as HTMLSelectElement).value).toBe(before);
});

it("still shows the pick after the catalogue is re-read", async () => {
  const port = new MockPort() as unknown as AgentPort;
  draw(port);
  const select = (await screen.findByRole("combobox", { name: "默认模型" })) as HTMLSelectElement;
  const picked = await pickAnother(select);
  // Without the reload the row keeps reading the catalogue's previous default
  // and the controlled select snaps back to it.
  const after = (await screen.findByRole("combobox", { name: "默认模型" })) as HTMLSelectElement;
  expect(after.value).toBe(picked);
});
