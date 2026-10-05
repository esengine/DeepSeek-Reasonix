// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { RailSearch } from "./railsearch";
import { RemoteHosts } from "./RemoteHosts";
import { useMachineBooks } from "./machinebooks";
import type { RemoteHost } from "../port/remote";
import type { HubPort, TreeWorkspace } from "../port/hub";
// Not through import.meta.glob: vite's CSS handling intercepts ?raw there, and
// the same import style is what the stylesheet guards use.
import appSource from "./App.tsx?raw";

afterEach(cleanup);

const host: RemoteHost = {
  name: "gpu",
  target: "ada@10.0.0.4",
  status: "connected",
  workspaces: ["/home/ada/training"],
};

// The far kernel names a session when its first turn ends, so the book read at
// connect holds the name the session had before it had one.
function farKernel() {
  const book: TreeWorkspace[] = [
    {
      root: "/home/ada/training",
      name: "training",
      sessions: [{ path: "/home/ada/training/s1.jsonl", name: "s1", title: "" }],
    },
  ];
  const hub = { remoteTree: vi.fn(async () => structuredClone(book)) } as unknown as HubPort;
  return { hub, names(title: string) { book[0].sessions[0].title = title; } };
}

// The window's own arrangement: the books belong to the owner, the column draws
// what it is handed, and one call re-reads them.
function Rail({ hub }: { hub: HubPort }) {
  const { trees, reload } = useMachineBooks(hub, [host]);
  return (
    <RailSearch>
      <button onClick={() => void reload()}>再读一次</button>
      <RemoteHosts
        hub={hub}
        hosts={[host]}
        runtimes={[]}
        active=""
        onOpen={async () => {}}
        onFocus={() => {}}
        reload={async () => {}}
        trees={trees}
        reloadTrees={reload}
        onClose={async () => {}}
        liveIds={() => []}
        readTree={async () => {}}
        onError={() => {}}
      />
    </RailSearch>
  );
}

describe("a remote session's title", () => {
  it("is read from the machine that owns it, as soon as it connects", async () => {
    const far = farKernel();
    far.names("把这个仓库的构建跑通");
    render(<Rail hub={far.hub} />);
    expect(await screen.findByText("把这个仓库的构建跑通")).toBeTruthy();
  });

  it("arrives on the re-read, rather than keeping the name from before it had one", async () => {
    const far = farKernel();
    render(<Rail hub={far.hub} />);
    await screen.findByText("training");
    expect(screen.queryByText("把这个仓库的构建跑通")).toBeNull();

    far.names("把这个仓库的构建跑通");
    await userEvent.click(screen.getByText("再读一次"));
    await waitFor(() => expect(screen.queryByText("把这个仓库的构建跑通")).toBeTruthy());
  });

  // The defect was never in the reading — it was that nothing asked. This
  // machine's tree is re-read when the pane list comes back, which is how the
  // window hears that a turn ended; a far book has to be re-read on that same
  // call or it keeps a name that is one turn old for as long as the pane lives.
  it("is re-read on the same beat this machine's tree is", () => {
    const panes = appSource.match(/const reloadPanes = useCallback\([\s\S]*?\}, \[[^\]]*\]\);/)?.[0] ?? "";
    expect(panes, "reloadPanes is not where it was").not.toBe("");
    expect(panes).toContain("reloadTree()");
    expect(panes, "the far books are not re-read when the panes come back").toContain("reloadRemoteTrees()");
  });
});
