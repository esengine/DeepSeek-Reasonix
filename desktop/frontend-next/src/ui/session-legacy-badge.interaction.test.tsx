// @vitest-environment jsdom
import { afterEach, describe, expect, it } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import "./testkit";
import { Workspaces } from "./Workspaces";
import type { TreeWorkspace } from "../port/hub";

afterEach(cleanup);

function draw(sessions: TreeWorkspace["sessions"]) {
  return render(
    <Workspaces
      hub={{} as never}
      tree={[{ root: "/w", name: "w", sessions } as TreeWorkspace]}
      treeRead
      runtimes={[]}
      active=""
      folded={new Set()}
      onFold={() => {}}
      reload={() => Promise.resolve()}
      onOpen={() => Promise.resolve()}
      onFocus={() => {}}
      onClose={() => Promise.resolve()}
      liveIds={() => []}
      runs={{}}
      onRename={() => {}}
      onError={() => {}}
      adder={{ add: () => {}, close: () => {}, at: null } as never}
    />,
  );
}

// A conversation 1.x kept reads here but is never written, so the row says so
// rather than looking like one of this build's own.
describe("a conversation kept by Reasonix 1.x", () => {
  it("marks the row with the line it came from", () => {
    draw([{ path: "/w/.reasonix/sessions/old.jsonl", name: "old", title: "旧会话", legacy: true }]);
    expect(screen.getByText("1.x")).toBeTruthy();
  });

  it("leaves this build's own conversation unmarked", () => {
    draw([{ path: "/w/.reasonix/sessions/mine.jsonl", name: "mine", title: "新会话" }]);
    expect(screen.queryByText("1.x")).toBeNull();
  });
});
