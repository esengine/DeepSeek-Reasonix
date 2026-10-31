import { describe, expect, it } from "vitest";
import { remoteWorkspaces } from "./RemoteHosts";
import { workspacesOf } from "./Remotes";
import type { TreeWorkspace } from "../port/hub";
import type { RemoteHost } from "../port/remote";

const host = (over: Partial<RemoteHost> = {}): RemoteHost => ({
  name: "gpu-box",
  target: "ada@10.0.0.4",
  status: "idle",
  ...over,
});

const far = (root: string, sessions = 0): TreeWorkspace => ({
  root,
  name: root.split("/").pop() ?? root,
  sessions: Array.from({ length: sessions }, (_, i) => ({ path: `${root}/s${i}.jsonl`, name: `s${i}` })),
});

const roots = (list: TreeWorkspace[]) => list.map((ws) => ws.root);

// One machine holds several projects. The book is what this window can offer
// before there is a link to ask through; the far kernel answers for what it
// remembers once there is. Neither alone is the list a reader needs.
describe("the folders listed under a remote machine", () => {
  it("lists every project in the book while nothing is connected", () => {
    const cold = remoteWorkspaces(host({ workspaces: ["/srv/train", "/srv/eval"] }), undefined);
    expect(roots(cold)).toEqual(["/srv/train", "/srv/eval"]);
    expect(cold[0].name, "a row is named by its folder, not by its path").toBe("train");
  });

  it("prefers the far kernel's own answer, which is the one with the sessions", () => {
    const warm = remoteWorkspaces(host({ workspaces: ["/srv/train"] }), [far("/srv/train", 3)]);
    expect(warm).toHaveLength(1);
    expect(warm[0].sessions).toHaveLength(3);
  });

  it("keeps a booked worktree inside the far kernel's original project", () => {
    const project = { ...far("/srv/train", 2), workspaceRoots: ["/srv/train", "/state/worktrees/train"] };
    const warm = remoteWorkspaces(host({ workspaces: ["/state/worktrees/train"] }), [project]);
    expect(roots(warm)).toEqual(["/srv/train"]);
    expect(warm[0].sessions).toHaveLength(2);
  });

  it("keeps a folder added here that has never been opened over there", () => {
    // Otherwise adding a project makes it vanish the moment the host connects.
    const warm = remoteWorkspaces(host({ workspaces: ["/srv/train", "/srv/eval"] }), [far("/srv/train", 2)]);
    expect(roots(warm)).toEqual(["/srv/train", "/srv/eval"]);
    expect(warm[1].sessions).toEqual([]);
  });

  it("keeps a folder opened over there that was never written down here", () => {
    const warm = remoteWorkspaces(host({ workspaces: ["/srv/train"] }), [far("/srv/train"), far("/srv/ad-hoc")]);
    expect(roots(warm)).toEqual(["/srv/train", "/srv/ad-hoc"]);
  });

  // The far machine is not necessarily this one: a Windows host answers with
  // backslashes, and splitting on "/" alone left the whole path as the label.
  it("names a Windows folder by its last segment too", () => {
    const cold = remoteWorkspaces(host({ workspaces: ["C:\\work\\pipeline\\"] }), undefined);
    expect(cold[0].name).toBe("pipeline");
  });

  it("has nothing to list for a machine with no folder written down", () => {
    expect(remoteWorkspaces(host(), undefined)).toEqual([]);
  });
});

// A row saved before the list existed still names its one folder. Reading the
// two fields apart is what would make that row a case every caller repeats.
describe("a host row that predates the list", () => {
  it("reads its single workspace as a list of one", () => {
    expect(workspacesOf(host({ workspace: "/srv/training" }))).toEqual(["/srv/training"]);
  });

  it("prefers the list once the kernel sends one", () => {
    expect(workspacesOf(host({ workspace: "/srv/train", workspaces: ["/srv/train", "/srv/eval"] }))).toEqual([
      "/srv/train",
      "/srv/eval",
    ]);
  });
});

describe("removed remote directories", () => {
  it("filters refreshed history and leftover panes without hiding sibling projects or other hosts", () => {
    const tree = [far("/root", 1), far("/srv/keep", 1)];
    const removed = host({ hiddenWorkspaces: ["/root"] });
    const pane = { id: "r", host: removed.name, root: "/root", name: "old", base: "" };
    expect(roots(remoteWorkspaces(removed, tree, [pane]))).toEqual(["/srv/keep"]);
    expect(remoteWorkspaces(removed, undefined, [pane])).toEqual([]);
    expect(roots(remoteWorkspaces(host({ name: "other" }), tree))).toEqual(["/root", "/srv/keep"]);
  });

  it("restores a grouped project when one of its worktrees is explicitly added again", () => {
    const tree = [{ ...far("/srv/train", 1), workspaceRoots: ["/srv/train", "/worktrees/train"] }];
    const removed = host({ hiddenWorkspaces: ["/srv/train", "/worktrees/train"] });
    expect(remoteWorkspaces(removed, tree)).toEqual([]);
    expect(roots(remoteWorkspaces({ ...removed, workspaces: ["/worktrees/train"], hiddenWorkspaces: ["/srv/train"] }, tree))).toEqual(["/srv/train"]);
  });
});
