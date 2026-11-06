// @vitest-environment jsdom
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, render } from "@testing-library/react";
import "./testkit";
import { Transcript } from "./Transcript";
import { STORAGE, boot } from "../i18n";
import type { Item } from "../state/session";

// Nothing is near but the last block, so every other block is a placeholder
// until a selection asks for it.
beforeAll(() => {
  globalThis.IntersectionObserver = class {
    readonly root = null;
    readonly rootMargin = "";
    readonly thresholds: readonly number[] = [];
    readonly scrollMargin = "";
    observe() {}
    unobserve() {}
    disconnect() {}
    takeRecords() { return []; }
  } as unknown as typeof IntersectionObserver;
});

beforeEach(() => {
  localStorage.clear();
  localStorage.setItem(STORAGE, "zh");
  boot();
});
afterEach(() => {
  document.getSelection()?.removeAllRanges();
  cleanup();
});

const noop = async () => undefined as never;

function transcript(items: Item[], onWithdraw: (id: string) => void = () => {}) {
  return (
    <Transcript
      items={items} entering={[]} onEntered={() => {}} revision={1} waiting={{}} scroll={{ current: null }}
      hidden={false} focus={null} onApprove={noop} onFullAccess={noop} onPlan={noop}
      onAnswer={noop} onForget={noop} onExtInvoke={() => {}} onExtSubmit={noop} checkpoints={new Map()}
      onPrepareRewind={noop} onCommitRewind={noop} onPrepareFileRevert={noop}
      onCommitFileRevert={noop} needsProject={false} onOpenProject={() => {}} onKeepHere={() => {}} onWithdraw={onWithdraw}
    />
  );
}


const steer: Item = { t: "user", id: "w", text: "测试也要覆盖带 BOM 的文件", pending: true, queued: "steer", itemId: "i1" };

describe("guidance waiting for a tool boundary", () => {
  it("stands after the whole conversation, outside the running turn", () => {
    const items: Item[] = [
      { t: "user", id: "u0", text: "question" },
      { t: "say", id: "s0", text: "answer", done: true },
      steer,
    ];
    const { container } = render(transcript(items));
    const bubble = container.querySelector(".steerwait")!;
    expect(bubble).toBeTruthy();
    const order = [...container.querySelectorAll(".call[data-k=me], .steerwait")];
    expect(order.at(-1)).toBe(bubble);
    expect(container.querySelectorAll(".call[data-k=me]")).toHaveLength(1);
  });

  it("stays at the end while a sentence is still streaming", () => {
    const items: Item[] = [
      { t: "user", id: "u0", text: "question" },
      { t: "say", id: "s0", text: "partial", done: false },
      steer,
    ];
    const { container } = render(transcript(items));
    const flow = container.querySelector(".flow-end")!;
    const bubble = container.querySelector(".steerwait")!;
    expect(bubble.compareDocumentPosition(flow) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    expect(container.textContent!.indexOf("partial")).toBeLessThan(container.textContent!.indexOf("测试也要覆盖"));
  });

  it("hands the withdrawal the entry the kernel named", () => {
    const onWithdraw = vi.fn();
    const { container } = render(transcript([steer], onWithdraw));
    container.querySelector<HTMLButtonElement>(".sw-undo")!.click();
    expect(onWithdraw).toHaveBeenCalledWith("i1");
  });

  it("draws a queued follow-up nowhere in the conversation", () => {
    const { container } = render(transcript([{ ...steer, queued: "followup" }]));
    expect(container.querySelector(".steerwait")).toBeNull();
    expect(container.textContent).not.toContain("测试也要覆盖");
  });
});
