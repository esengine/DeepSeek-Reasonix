// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import "./testkit";
import { RailSearch } from "./railsearch";
import { RemoteHosts } from "./RemoteHosts";
import { useMachineBooks } from "./machinebooks";
import type { RemoteHost } from "../port/remote";
import type { HubPort, TreeWorkspace } from "../port/hub";

afterEach(cleanup);

const host: RemoteHost = { name: "gpu", target: "ada@10.0.0.4", status: "connected", workspaces: ["/home/ada/training"] };

function Rail({ book }: { book: TreeWorkspace[] }) {
  const hub = { remoteTree: vi.fn(async () => structuredClone(book)) } as unknown as HubPort;
  const { trees, reload } = useMachineBooks(hub, [host]);
  return (
    <RailSearch>
      <RemoteHosts hub={hub} hosts={[host]} runtimes={[]} active="" onOpen={async () => {}} onFocus={() => {}} reload={async () => {}}
        trees={trees} reloadTrees={reload} onClose={async () => {}} liveIds={() => []} readTree={async () => {}} onError={() => {}} />
    </RailSearch>
  );
}

const sessions = (n: number) => Array.from({ length: n }, (_, i) => ({ path: `/home/ada/training/s${i}.jsonl`, name: `s${i}`, title: `run ${i}` }));

it("draws every row the far kernel sent, with a pointer to the rest and no count on the project", async () => {
  render(<Rail book={[{ root: "/home/ada/training", name: "training", total: 400, sessions: sessions(50) }]} />);
  expect(await screen.findAllByText(/^run \d+$/)).toHaveLength(50);
  expect(document.querySelector(".rowcount")).toBeNull();
  expect(screen.getByText("还有 350 个较早的会话，在那台机器上查看")).toBeTruthy();
});

it("points to nothing more for a far kernel that sends no total", async () => {
  render(<Rail book={[{ root: "/home/ada/training", name: "training", sessions: sessions(7) }]} />);
  expect(await screen.findAllByText(/^run \d+$/)).toHaveLength(7);
  expect(screen.queryByText(/较早的会话/)).toBeNull();
});
