// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { cleanup, render } from "@testing-library/react";
import "./testkit";
import { Transcript } from "./Transcript";
import { FOLD_DEFAULTS, foldModes, setFoldModes } from "../state/prefs";
import type { Item } from "../state/session";

beforeEach(() => setFoldModes(FOLD_DEFAULTS));
afterEach(() => {
  cleanup();
  setFoldModes(FOLD_DEFAULTS);
});

const user = (id: string, text: string): Item => ({ t: "user", id, text });
const say = (id: string, text: string): Item => ({ t: "say", id, text, done: true });
const bash = (id: string): Item => ({
  t: "tool", id, running: false, children: [],
  tool: { id, name: "bash", args: '{"command":"ls"}', output: "a.ts", readOnly: false },
});
const edit = (id: string): Item => ({
  t: "tool", id, running: false, children: [],
  tool: { id, name: "edit_file", args: '{"path":"src/a.ts"}', diff: "@@ -1 +1 @@\n-old\n+new", readOnly: false },
});

const noop = async () => undefined as never;

function draw(items: Item[]) {
  render(
    <Transcript
      items={items}
      entering={[]}
      onEntered={() => {}}
      revision={1}
      waiting={{}}
      scroll={{ current: null }}
      hidden={false}
      onPinned={() => {}}
      jump={0}
      focus={null}
      onApprove={noop}
      onFullAccess={noop}
      onPlan={noop}
      onAnswer={noop}
      onForget={noop}
      onExtInvoke={() => {}}
      onExtSubmit={noop}
      checkpoints={new Map()}
      onPrepareRewind={noop}
      onCommitRewind={noop}
      onUndoRewind={noop}
      onPrepareFileRevert={noop}
      onCommitFileRevert={noop}
      needsProject={false}
      onOpenProject={() => {}}
      onKeepHere={() => {}}
    />,
  );
}

const disclosure = (id: string) =>
  document.querySelector(`[data-item="${id}"] details.tool-disclosure`) as HTMLDetailsElement;

// A reader who only audits what was written wants every diff laid open after
// the turn, and every lookup left as one line.
describe("folding that opens file changes only", () => {
  it("is a mode the settings keep", () => {
    setFoldModes({ activity: "changed", steps: "changed" });
    expect(foldModes().activity).toBe("changed");
    expect(foldModes().steps).toBe("changed");
  });

  it("opens the work and the steps that changed a file, and nothing else", () => {
    setFoldModes({ activity: "changed", steps: "changed" });
    draw([
      user("u", "改一下"),
      say("s1", "先看看"),
      bash("look"),
      say("s2", "现在改"),
      bash("check"),
      edit("write"),
      say("s3", "好了"),
    ]);

    const groups = [...document.querySelectorAll<HTMLDetailsElement>("details.activity-group")];
    expect(groups.map((g) => g.open)).toEqual([false, true]);
    expect(disclosure("write").open).toBe(true);
    expect(disclosure("check").open).toBe(false);
  });
});
