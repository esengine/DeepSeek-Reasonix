// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { Remotes } from "./Remotes";
import type { HubPort } from "../port/hub";
import type { RemoteHost, RemoteHostEdit } from "../port/remote";

afterEach(cleanup);

const host: RemoteHost = {
  name: "gpu-box", target: "ada@10.0.0.4", status: "idle", workspaces: [],
};

function draw(saveRemoteHost = vi.fn(async (_entry: RemoteHostEdit): Promise<RemoteHost[]> => [])) {
  const hub = {
    remoteHosts: async () => [host],
    remoteCandidates: async () => [],
    wslDistros: async () => ({ available: false, distros: [] }),
    saveRemoteHost,
  } as unknown as HubPort;
  render(<Remotes hub={hub} onError={() => {}} />);
  return saveRemoteHost;
}

// The switch beside a row decides whether the machine is dialed at all. It is the
// row's only switch: whether the rail lists it is a separate preference, and a
// separate row action, because that one commits on click while a control inside
// the edit form would only commit on Save.
describe("the menu item that turns a machine off", () => {
  it("saves the whole row with the flag flipped", async () => {
    const save = draw();
    await userEvent.click(await screen.findByRole("button", { name: /^更多：/ }));
    await userEvent.click(screen.getByRole("menuitem", { name: "停用这台主机" }));
    await waitFor(() => expect(save).toHaveBeenCalled());
    const entry = save.mock.calls[0][0] as RemoteHostEdit;
    expect(entry.name).toBe("gpu-box");
    expect(entry.disabled).toBe(true);
  });

  it("keeps the hide preference to one control, out of the edit form", async () => {
    draw();
    await userEvent.click(await screen.findByRole("button", { name: /^更多：/ }));
    expect(document.querySelectorAll('[data-action="remote.hide"]')).toHaveLength(1);
  });
});

describe("a machine card", () => {
  it("names the link in one status pill and offers the next step on the card, not a switch", async () => {
    draw();
    await screen.findByText("gpu-box");
    expect(document.querySelector(".rmtrow .st")?.getAttribute("data-state")).toBe("idle");
    expect(document.querySelectorAll('.rmtrow > .rmtside [data-action="remote.probe"]')).toHaveLength(1);
    expect(screen.queryByRole("switch")).toBeNull();
  });

  it("puts the add button above the list", async () => {
    draw();
    await screen.findByText("gpu-box");
    const add = document.querySelector(".rmthead .rmtadd");
    expect(add).not.toBeNull();
    expect(document.querySelectorAll(".rmtbook > .rmtadd")).toHaveLength(0);
  });
});

describe("the machines found in ~/.ssh/config", () => {
  it("adds every one of them from one button, each through the ssh config", async () => {
    const saveRemoteHost = vi.fn(async (_entry: RemoteHostEdit): Promise<RemoteHost[]> => []);
    const hub = {
      remoteHosts: async () => [host],
      remoteCandidates: async () => ["attic", "old-nas"],
      wslDistros: async () => ({ available: false, distros: [] }),
      saveRemoteHost,
    } as unknown as HubPort;
    render(<Remotes hub={hub} onError={() => {}} />);
    await userEvent.click(await screen.findByRole("button", { name: "全部添加" }));
    await waitFor(() => expect(saveRemoteHost).toHaveBeenCalledTimes(2));
    expect(saveRemoteHost.mock.calls.map(([e]) => [e.name, e.useSSHConfig])).toEqual([["attic", true], ["old-nas", true]]);
  });
});
