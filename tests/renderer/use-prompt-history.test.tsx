// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React from "react";
import { describe, expect, it } from "vitest";
import { type PromptHistory, usePromptHistory } from "../../src/cli/ui/use-prompt-history.js";
import { CharPool, HyperlinkPool, StylePool, mount } from "../../src/renderer/index.js";
import { makeTestWriter } from "../../src/renderer/runtime/test-writer.js";

function pools() {
  return { char: new CharPool(), style: new StylePool(), hyperlink: new HyperlinkPool() };
}

function flush(): Promise<void> {
  return new Promise((r) => setTimeout(r, 0));
}

/** Mount a tiny probe that exposes the hook's API to the surrounding test. */
async function buildHistory(): Promise<{ history: PromptHistory; destroy: () => void }> {
  let captured: PromptHistory | null = null;
  function Probe(): React.ReactElement | null {
    captured = usePromptHistory(5);
    return null;
  }
  const w = makeTestWriter();
  const handle = mount(<Probe />, {
    viewportWidth: 20,
    viewportHeight: 4,
    pools: pools(),
    write: w.write,
  });
  await flush();
  if (!captured) throw new Error("hook was never invoked");
  return { history: captured, destroy: () => handle.destroy() };
}

describe("usePromptHistory — empty state", () => {
  it("recallPrev on an empty store returns null", async () => {
    const { history, destroy } = await buildHistory();
    expect(history.recallPrev("draft")).toBeNull();
    destroy();
  });

  it("recallNext when not recalling returns null", async () => {
    const { history, destroy } = await buildHistory();
    expect(history.recallNext()).toBeNull();
    destroy();
  });
});

describe("usePromptHistory — recording + recall", () => {
  it("recordSubmit then recallPrev returns the most recent entry", async () => {
    const { history, destroy } = await buildHistory();
    history.recordSubmit("first");
    history.recordSubmit("second");
    expect(history.recallPrev("draft")).toBe("second");
    destroy();
  });

  it("recallPrev twice walks back through history", async () => {
    const { history, destroy } = await buildHistory();
    history.recordSubmit("one");
    history.recordSubmit("two");
    history.recordSubmit("three");
    expect(history.recallPrev("draft")).toBe("three");
    expect(history.recallPrev("draft")).toBe("two");
    expect(history.recallPrev("draft")).toBe("one");
    expect(history.recallPrev("draft")).toBeNull();
    destroy();
  });

  it("recallNext walks forward + restores the saved draft past the newest", async () => {
    const { history, destroy } = await buildHistory();
    history.recordSubmit("one");
    history.recordSubmit("two");
    expect(history.recallPrev("my draft")).toBe("two"); // saves draft
    expect(history.recallPrev("ignored")).toBe("one");
    expect(history.recallNext()).toBe("two");
    expect(history.recallNext()).toBe("my draft"); // back to original draft
    expect(history.recallNext()).toBeNull(); // not recalling anymore
    destroy();
  });

  it("duplicate-of-latest submissions are coalesced", async () => {
    const { history, destroy } = await buildHistory();
    history.recordSubmit("hi");
    history.recordSubmit("hi");
    expect(history.recallPrev("d")).toBe("hi");
    expect(history.recallPrev("d")).toBeNull();
    destroy();
  });

  it("empty submissions are ignored", async () => {
    const { history, destroy } = await buildHistory();
    history.recordSubmit("");
    expect(history.recallPrev("d")).toBeNull();
    destroy();
  });

  it("entries past maxEntries are dropped from the front", async () => {
    const { history, destroy } = await buildHistory();
    // maxEntries=5 from the harness; push 7 distinct entries.
    for (let i = 1; i <= 7; i++) history.recordSubmit(`m${i}`);
    expect(history.recallPrev("d")).toBe("m7");
    expect(history.recallPrev("d")).toBe("m6");
    expect(history.recallPrev("d")).toBe("m5");
    expect(history.recallPrev("d")).toBe("m4");
    expect(history.recallPrev("d")).toBe("m3");
    expect(history.recallPrev("d")).toBeNull(); // m1 + m2 evicted
    destroy();
  });
});

describe("usePromptHistory — reset", () => {
  it("recordSubmit clears any in-progress recall", async () => {
    const { history, destroy } = await buildHistory();
    history.recordSubmit("a");
    history.recordSubmit("b");
    history.recallPrev("draft"); // index = 1, draft saved
    history.recordSubmit("c"); // resets cursor
    // After recordSubmit, recallPrev should start fresh from the newest entry.
    expect(history.recallPrev("new draft")).toBe("c");
    destroy();
  });

  it("explicit reset() clears recall state", async () => {
    const { history, destroy } = await buildHistory();
    history.recordSubmit("a");
    history.recallPrev("draft");
    history.reset();
    expect(history.recallNext()).toBeNull();
    destroy();
  });
});
