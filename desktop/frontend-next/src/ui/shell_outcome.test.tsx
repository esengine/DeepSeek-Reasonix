// @vitest-environment jsdom
import { afterEach, describe, expect, it } from "vitest";
import { cleanup, render } from "@testing-library/react";
import "./testkit";
import { Transcript } from "./Transcript";
import { fromHistory, type Item } from "../state/session";
import { toolFailed } from "./cards/outcome";
import type { Execution, Tool } from "../port/wire";

afterEach(cleanup);

const user = (id: string, text: string): Item => ({ t: "user", id, text });
const say = (id: string, text: string): Item => ({ t: "say", id, text, done: true });

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
      onPrepareFileRevert={noop}
      onCommitFileRevert={noop}
      needsProject={false}
      onOpenProject={() => {}}
      onKeepHere={() => {}}
    />,
  );
  return document.querySelector(".chunk") as HTMLElement;
}


const shell = (id: string, execution: Execution, over: Partial<Tool> = {}): Item => ({
  t: "tool", id, running: false, children: [],
  tool: { id, name: "bash", args: '{"command":"cd \\"/work/example\\" && make serve"}', readOnly: false, execution: { kind: "shell", shell: "git-bash", ...execution }, ...over },
});
const waiting = (id: string, over: Partial<Tool> = {}, running = true): Item => ({ t: "tool", id, tool: { id, name: "wait", readOnly: true, ...over }, running, children: [] });
const started: Execution = { state: "background_started" };

describe("a shell call's class is the host's, not the shape of its execution record", () => {
  it.each([
    ["a background start", started, {}, false],
    ["a clean exit", { state: "completed", exitCode: 0 }, {}, false],
    ["a non-zero exit", { state: "failed", exitCode: 2 }, { err: "command exited: exit status 2" }, true],
    ["a timeout", { state: "timed_out" }, { err: "command timed out" }, true],
    ["a user stop", { state: "cancelled" }, { err: "context canceled" }, true],
    ["a call that never ran", { state: "not_run" }, { err: "blocked by permission policy", refusalCode: "permission.denied" }, true],
  ] as const)("%s", (_, execution, over, bad) => {
    expect(toolFailed({ id: "c", name: "bash", readOnly: false, execution, ...over })).toBe(bad);
  });

  it("draws a background start as settled, with no failure mark", () => {
    const chunk = draw([user("u", "serve it"), say("s", "starting"), shell("b1", started), waiting("w1")]);
    const group = chunk.querySelector(".activity-group")!;
    expect(group.querySelector(".fail")).toBeNull();
    expect(group.querySelector(".activity-errors")).toBeNull();
    expect(group.hasAttribute("data-failed")).toBe(false);
    expect(group.querySelector('[data-state="failed"]')).toBeNull();
    expect(group.textContent).toContain("1 项运行中");
    expect(group.textContent).not.toContain("background_started");
  });

  it("still counts and labels the calls that did fail", () => {
    const chunk = draw([
      user("u", "go"), say("s", "working"),
      shell("b1", started),
      shell("b2", { state: "failed", exitCode: 2 }, { err: "command exited: exit status 2" }),
      shell("b3", { state: "timed_out" }, { err: "command timed out" }),
      waiting("w1"),
    ]);
    const group = chunk.querySelector(".activity-group")!;
    expect(group.querySelector(".activity-errors")?.textContent).toBe("2 项失败");
    expect([...group.querySelectorAll(".fail")].map((e) => e.textContent)).toEqual(["exit 2", "timed_out"]);
  });

  it("keeps the wait row running and its later failure visible", () => {
    const chunk = draw([user("u", "go"), say("s", "working"), shell("b1", started), waiting("w1", { err: "job exited with status 1" }, false)]);
    const group = chunk.querySelector(".activity-group")!;
    expect(group.querySelector(".activity-errors")?.textContent).toBe("1 项失败");
    expect(group.querySelector('[data-state="failed"]')).not.toBeNull();
  });
});

describe("a reopened session classifies the same way", () => {
  const rebuilt = (failed: boolean) =>
    fromHistory([
      { role: "assistant", content: "", msgIndex: 0, toolCalls: [{ id: "c1", name: "bash", arguments: '{"command":"make serve","run_in_background":true}' }] },
      { role: "tool", content: failed ? "error: exit 1" : 'Started background job "j1".', msgIndex: 1, toolCallId: "c1", toolName: "bash", ...(failed ? { toolFailed: true } : {}) },
    ]).items.find((i) => i.t === "tool") as Extract<Item, { t: "tool" }>;

  it("a recorded background start is not a failure", () => expect(toolFailed(rebuilt(false).tool)).toBe(false));
  it("a recorded failure still is", () => expect(toolFailed(rebuilt(true).tool)).toBe(true));
});
