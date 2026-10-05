// @vitest-environment jsdom
import { afterEach, describe, expect, it } from "vitest";
import { cleanup, render } from "@testing-library/react";
import "./testkit";
import { Transcript } from "./Transcript";
import type { Item } from "../state/session";

afterEach(cleanup);

// Find and the rail both land on a row by its item id. A row that does not
// carry it cannot be painted, stepped to, or reached from the rail — which is
// exactly how all three stopped working at once.
describe("a transcript row", () => {
  it("carries its item id, work under a sentence included", () => {
    const items: Item[] = [
      { t: "user", id: "u1", text: "hi", authoredTurn: 1, msgIndex: 0 },
      { t: "say", id: "s1", text: "ok", done: true },
      { t: "tool", id: "t1", running: false, children: [], tool: { id: "t1", name: "bash", args: "{}", readOnly: false } },
    ];
    const noop = async () => undefined as never;
    render(
      <Transcript items={items} entering={[]} onEntered={() => {}} revision={1} waiting={{}} scroll={{ current: null }} hidden={false}
        onPinned={() => {}} jump={0} focus={null} onApprove={noop} onFullAccess={noop} onPlan={noop} onAnswer={noop} onForget={noop}
        onExtInvoke={() => {}} onExtSubmit={noop} checkpoints={new Map()} onPrepareRewind={noop} onCommitRewind={noop} onUndoRewind={noop}
        onPrepareFileRevert={noop} onCommitFileRevert={noop} needsProject={false} onOpenProject={() => {}} onKeepHere={() => {}} />,
    );
    for (const id of ["u1", "s1", "t1"]) expect(document.querySelector(`[data-item="${id}"]`), id).not.toBeNull();
  });
});
