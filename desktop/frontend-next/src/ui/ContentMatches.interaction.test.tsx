// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { HistoryHit, HistorySearch, TreeWorkspace } from "../port/hub";
import { ContentMatches } from "./ContentMatches";
import { RailSearch } from "./railsearch";

const tree = [{ root: "/w/a", name: "a", sessions: [{ path: "/w/a/s1.jsonl", name: "s1", title: "Rotate keys" }] }] as unknown as TreeWorkspace[];
const hit: HistoryHit = { path: "/w/a/s1.jsonl", id: "s1", snippet: "rotate the zebra credentials", role: "user", messageIndex: 0, score: 1 };

function answer(hits: HistoryHit[], index: Partial<HistorySearch["index"]> = {}): HistorySearch {
  return { hits, index: { state: "ready", indexed: 1, total: 1, failed: 0, partial: false, ...index } };
}

function setup(search: (q: string) => Promise<HistorySearch>, workspaces = tree, onReference?: (id: string, title: string) => void) {
  const onOpen = vi.fn(() => Promise.resolve());
  render(
    <RailSearch>
      <ContentMatches hub={{ searchHistory: search }} tree={workspaces} onOpen={onOpen} onError={() => {}} onReference={onReference} />
    </RailSearch>,
  );
  return onOpen;
}

async function ask(value = "zebra") {
  fireEvent.change(screen.getByRole("searchbox"), { target: { value } });
  await act(async () => void vi.advanceTimersByTime(400));
}

describe("ContentMatches", () => {
  beforeEach(() => vi.useFakeTimers());
  afterEach(() => {
    cleanup();
    vi.useRealTimers();
  });

  it("asks only after the reader pauses on two or more characters, then opens the match in its workspace", async () => {
    const search = vi.fn(() => Promise.resolve(answer([hit])));
    const onOpen = setup(search);
    const box = screen.getByRole("searchbox");

    fireEvent.change(box, { target: { value: "z" } });
    await act(async () => void vi.advanceTimersByTime(1000));
    expect(search).not.toHaveBeenCalled();

    fireEvent.change(box, { target: { value: "zebra" } });
    await act(async () => void vi.advanceTimersByTime(100));
    expect(search).not.toHaveBeenCalled();
    await act(async () => void vi.advanceTimersByTime(300));
    expect(search).toHaveBeenCalledWith("zebra");

    fireEvent.click(document.querySelector("[data-action='session.open-match']")!);
    expect(screen.getByText("Rotate keys")).toBeTruthy();
    expect(onOpen).toHaveBeenCalledWith({ root: "/w/a", sessionPath: "/w/a/s1.jsonl" });
  });

  it("says so when the index cannot answer instead of showing nothing", async () => {
    setup(() => Promise.reject(new Error("down")));
    await ask();
    expect(document.querySelector("[data-state='failed']")).not.toBeNull();
  });

  it("says a session could not be indexed instead of showing an empty result", async () => {
    setup(() => Promise.resolve(answer([], { partial: true, reason: "failed", failed: 1, indexed: 1, total: 2 })));
    await ask();
    const line = document.querySelector("[data-state='incomplete']");
    expect(line?.getAttribute("data-reason")).toBe("failed");
    expect(line?.textContent).toBe("有些历史内容没能索引，搜索结果可能不完整");
    expect(document.querySelector("[data-action='session.open-match']")).toBeNull();
  });

  it("says nothing when a complete index found nothing", async () => {
    setup(() => Promise.resolve(answer([])));
    await ask();
    expect(document.querySelector(".content-matches")).toBeNull();
    expect(document.querySelector("[data-state='incomplete']")).toBeNull();
  });

  it("keeps the matches and the line when the index could not answer for everything", async () => {
    setup(() => Promise.resolve(answer([hit], { partial: true, reason: "failed", failed: 1, indexed: 1, total: 2 })));
    await ask();
    expect(document.querySelector("[data-action='session.open-match']")).not.toBeNull();
    expect(document.querySelector("p[data-state='incomplete']")?.getAttribute("data-reason")).toBe("failed");
  });

  it("shows the index's own progress while it is still building", async () => {
    setup(() => Promise.resolve(answer([], { partial: true, reason: "building", indexed: 1, total: 3 })));
    await ask();
    expect(document.querySelector("[data-state='incomplete']")?.textContent).toBe("历史索引还在建立（1/3），搜索结果可能不完整");
  });

  it("opens a grouped content match in the session's working directory", async () => {
    const onOpen = setup(() => Promise.resolve(answer([hit])), [{ ...tree[0], sessions: [
      { ...tree[0].sessions[0], workspaceRoot: "/worktrees/a" },
    ] }]);
    await ask();
    fireEvent.click(document.querySelector("[data-action='session.open-match']")!);
    expect(onOpen).toHaveBeenCalledWith({ root: "/worktrees/a", sessionPath: hit.path });
  });

  it("offers to reference a match in the message box, and referencing does not open it", async () => {
    const onReference = vi.fn();
    const onOpen = setup(() => Promise.resolve(answer([hit])), tree, onReference);
    await ask();
    fireEvent.click(screen.getByRole("button", { name: "引用「Rotate keys」到输入框" }));
    expect(onReference).toHaveBeenCalledExactlyOnceWith("s1", "Rotate keys");
    expect(onOpen).not.toHaveBeenCalled();
  });

  it("has no reference button when there is no message box to put it in", async () => {
    setup(() => Promise.resolve(answer([hit])));
    await ask();
    expect(document.querySelector("[data-action='session.reference-match']")).toBeNull();
  });
});
