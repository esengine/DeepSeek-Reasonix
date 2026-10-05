// @vitest-environment jsdom
import { afterEach, describe, expect, it } from "vitest";
import { cleanup, render } from "@testing-library/react";
import "./testkit";
import { Transcript } from "./Transcript";
import type { Item } from "../state/session";

afterEach(cleanup);

const user = (id: string, text: string): Item => ({ t: "user", id, text });
const say = (id: string, text: string): Item => ({ t: "say", id, text, done: true });
const call = (id: string, name: string): Item => ({ t: "tool", id, tool: { id, name, readOnly: true }, running: false, children: [] });

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
  return document.querySelector(".chunk") as HTMLElement;
}

// A turn answers more than once: the model speaks, works, speaks again. Each
// stretch of work belongs to the sentence that preceded it, so a reader finds
// the command under the thing it was for.
describe("the work under a turn's sentences", () => {
  it("opens a disclosure per stretch of work", () => {
    const chunk = draw([
      user("u", "做这件事"),
      say("s1", "先看看代码"),
      call("t1", "bash"),
      call("t2", "read_file"),
      say("s2", "现在改这里"),
      call("t3", "edit_file"),
      say("s3", "改好了"),
    ]);

    const groups = [...chunk.querySelectorAll(".activity-group")];
    expect(groups).toHaveLength(2);
    expect(groups[0].textContent).toContain("bash");
    expect(groups[1].textContent).toContain("edit_file");
  });

  it("keeps each sentence above the work that followed it", () => {
    const chunk = draw([
      user("u", "做这件事"),
      say("s1", "先看看代码"),
      call("t1", "bash"),
      say("s2", "现在改这里"),
      call("t2", "edit_file"),
      say("s3", "改好了"),
    ]);

    const shape = [...chunk.querySelectorAll<HTMLElement>('[data-k="say"], .activity-group')].map((el) => {
      if (el.classList.contains("activity-group")) return "work";
      const text = el.textContent ?? "";
      return text.includes("先看看代码") ? "s1" : text.includes("现在改这里") ? "s2" : "s3";
    });
    expect(shape).toEqual(["s1", "work", "s2", "work", "s3"]);
  });
});

// A mounting block closes only where a turn does, so a turn longer than one
// block keeps its work under its own sentences, and the rule between turns
// falls between turns.
describe("the blocks a transcript mounts in", () => {
  it("keeps a turn longer than a block whole", () => {
    const work = Array.from({ length: 60 }, (_, i) => call(`t${i}`, "bash"));
    draw([user("u1", "第一件"), say("s1", "开始"), ...work, say("s2", "做完了"), user("u2", "第二件"), say("s3", "好")]);

    const chunks = [...document.querySelectorAll(".chunk")];
    expect(chunks).toHaveLength(2);
    expect(chunks[0].querySelectorAll(".activity-group")).toHaveLength(1);
    expect(chunks[0].textContent).toContain("做完了");
    expect(chunks[1].textContent).toContain("第二件");
  });

  it("draws a rule before every turn and none before the first", () => {
    const turns = Array.from({ length: 30 }, (_, i) => [user(`u${i}`, `问题 ${i}`), say(`s${i}`, `回答 ${i}`)]).flat();
    draw(turns);

    const chunks = [...document.querySelectorAll(".chunk")];
    expect(chunks.length).toBeGreaterThan(1);
    for (const chunk of chunks) expect(chunk.firstElementChild?.className).toBe("turn-rule");
    expect(document.querySelectorAll(".turn-rule")).toHaveLength(30);
  });

  it("does not open a turn for a line steered into one", () => {
    draw([user("u1", "做这件事"), say("s1", "在做"), { t: "user", id: "st", text: "顺便看下测试", steer: true }, say("s2", "好")]);

    expect(document.querySelectorAll(".turn-rule")).toHaveLength(1);
  });
});
