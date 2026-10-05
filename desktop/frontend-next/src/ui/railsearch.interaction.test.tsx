// @vitest-environment jsdom
import { afterEach, describe, expect, it } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { RailSearch } from "./railsearch";
import { RemoteHosts } from "./RemoteHosts";
import type { RemoteHost } from "../port/remote";
import type { HubPort } from "../port/hub";

afterEach(cleanup);

const host = (name: string, over: Partial<RemoteHost> = {}): RemoteHost => ({
  name,
  target: `ada@${name}.internal`,
  status: "connected",
  workspaces: [`/home/ada/${name}-work`],
  ...over,
});

const hub = { remoteTree: async () => [] } as unknown as HubPort;

const draw = (hosts: RemoteHost[]) =>
  render(
    <RailSearch>
      <RemoteHosts
        hub={hub}
        hosts={hosts}
        runtimes={[]}
        active=""
        onOpen={async () => {}}
        onFocus={() => {}}
        reload={async () => {}}
        trees={{}}
        reloadTrees={async () => {}}
        onClose={async () => {}}
        liveIds={() => []}
        readTree={async () => {}}
        onError={() => {}}
      />
    </RailSearch>,
  );

const find = () => screen.getByRole("searchbox", { name: "搜索会话 / 项目" });

// The rail is a list of machines, so its search has to ask the whole list. It
// filtered only the local half: in two panels that read as a local search, and
// in one list it reads as a search that skips most of it — and the half it
// skips looks like a half that is not there.
describe("the rail's search reaches every machine", () => {
  it("keeps a machine whose name matches", async () => {
    draw([host("gpu"), host("builder")]);
    await userEvent.type(find(), "gpu");
    expect(screen.queryByText("gpu")).toBeTruthy();
    expect(screen.queryByText("builder")).toBeNull();
  });

  it("keeps a machine whose address matches, not only its name", async () => {
    draw([host("gpu", { target: "ada@10.0.0.4" }), host("builder")]);
    await userEvent.type(find(), "10.0.0");
    expect(screen.queryByText("gpu")).toBeTruthy();
    expect(screen.queryByText("builder")).toBeNull();
  });

  it("keeps a machine whose folder matches, and only that folder", async () => {
    draw([host("gpu", { workspaces: ["/home/ada/training", "/home/ada/scratch"] })]);
    await userEvent.type(find(), "train");
    expect(screen.queryByText("training")).toBeTruthy();
    expect(screen.queryByText("scratch")).toBeNull();
  });

  it("puts every machine back when the word is cleared", async () => {
    draw([host("gpu"), host("builder")]);
    await userEvent.type(find(), "gpu");
    expect(screen.queryByText("builder")).toBeNull();
    await userEvent.clear(find());
    expect(screen.queryByText("builder")).toBeTruthy();
  });

  // A fold is a resting-state preference, and while searching it hides the very
  // rows just found. The local half has always done this; both halves must.
  it("opens a folded machine rather than hiding what it just found", async () => {
    draw([host("gpu", { workspaces: ["/home/ada/training"] })]);
    await userEvent.click(screen.getByText("gpu"));
    expect(screen.queryByText("training")).toBeNull();
    await userEvent.type(find(), "train");
    expect(screen.queryByText("training")).toBeTruthy();
  });
});
