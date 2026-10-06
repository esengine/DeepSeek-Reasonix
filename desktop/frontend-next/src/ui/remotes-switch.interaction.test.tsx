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
    saveRemoteHost,
  } as unknown as HubPort;
  render(<Remotes hub={hub} onError={() => {}} />);
  return saveRemoteHost;
}

// The switch beside a row decides whether the machine is dialed at all. Whether the
// rail lists it is this window's preference rather than the machine's own setting, so
// it sits in the edit panel with the row's other details — the same toggle the row
// would call, and it still acts on click rather than on Save.
describe("the switch that turns a machine off", () => {
  it("saves the whole row with the flag flipped", async () => {
    const save = draw();
    await userEvent.click(await screen.findByRole("switch", { name: "停用这台主机" }));
    await waitFor(() => expect(save).toHaveBeenCalled());
    const entry = save.mock.calls[0][0] as RemoteHostEdit;
    expect(entry.name).toBe("gpu-box");
    expect(entry.disabled).toBe(true);
  });

  it("keeps the hide preference in the edit panel, not beside the dial switch", async () => {
    draw();
    await screen.findByRole("switch", { name: "停用这台主机" });
    expect(document.querySelectorAll('[data-action="remote.hide"]')).toHaveLength(0);
    await userEvent.click(await screen.findByRole("button", { name: "编辑" }));
    expect(document.querySelectorAll('[data-action="remote.hide"]')).toHaveLength(1);
  });
});
