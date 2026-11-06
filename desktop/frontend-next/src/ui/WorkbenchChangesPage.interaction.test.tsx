// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { WorkbenchChangesPage } from "./WorkbenchChangesPage";
import { diffLines } from "./UnifiedDiff";
import { MockPort } from "../port/mock";
import type { WorkspaceChange } from "../port/port";

afterEach(cleanup);

const DIFF = "diff --git a/x b/x\n--- a/x\n+++ b/x\n@@ -12,3 +12,3 @@ func Load\n key := 1\n-if key == \"\" {\n--- a comment that starts with dashes\n+if strings.TrimSpace(key) == \"\" {\n }\n";
const changes: WorkspaceChange[] = [
  { path: "internal/config/credentials.go", status: "M", staged: true, insertions: 6, deletions: 2 },
  { path: "internal/provider/retry.go", status: "M", unstaged: true, insertions: 14, deletions: 3 },
  { path: "docs/notes.md", status: "??", unstaged: true },
];

function draw(over: Partial<Parameters<typeof WorkbenchChangesPage>[0]> = {}) {
  const port = new MockPort();
  vi.spyOn(port, "changeDiff").mockResolvedValue({ path: "x", diff: DIFF, truncated: false });
  const stage = vi.spyOn(port, "stageChanges").mockResolvedValue(undefined as never);
  const discard = vi.spyOn(port, "discardChange").mockResolvedValue(undefined);
  const onOpen = vi.fn();
  render(<WorkbenchChangesPage port={port} changes={changes} branch="main" shown remote={false} onOpen={onOpen} {...over} />);
  return { port, stage, discard, onOpen };
}

describe("a unified diff, line by line", () => {
  it("numbers each line before and after, keeps the hunk header, and reads a removed '--' line as content", () => {
    const lines = diffLines(DIFF);
    expect(lines.map((l) => [l.mark, l.old ?? null, l.now ?? null])).toEqual([
      ["@", null, null], [" ", 12, 12], ["-", 13, null], ["-", 14, null], ["+", null, 13], [" ", 15, 14],
    ]);
    expect(lines[3].text).toBe("-- a comment that starts with dashes");
  });
});

describe("the changes page, as the WB-Frame board draws it", () => {
  it("heads with the branch, how many files and lines, and the three git actions", () => {
    draw();
    const head = document.querySelector(".wb-changes-head")!;
    expect(head.textContent).toContain("main");
    expect(head.textContent).toContain("3 个文件 +20 −5");
    expect(screen.getByRole("button", { name: "暂存全部" })).toBeTruthy();
  });

  it("shows an edited file's diff at once and folds an untracked one behind a line saying what it is", async () => {
    draw();
    await waitFor(() => expect(document.querySelectorAll(".wb-diff")).toHaveLength(2));
    expect(screen.getByRole("button", { name: "未跟踪的文件 · 点开看内容" })).toBeTruthy();
    await userEvent.click(screen.getByRole("button", { name: "未跟踪的文件 · 点开看内容" }));
    await waitFor(() => expect(document.querySelectorAll(".wb-diff")).toHaveLength(3));
  });

  it("stages a file from its header and asks once before throwing its change away", async () => {
    const { stage, discard, onOpen } = draw();
    await userEvent.click(screen.getByRole("checkbox", { name: "暂存 internal/provider/retry.go" }));
    expect(stage).toHaveBeenCalledWith({ paths: ["internal/provider/retry.go"], unstage: false });
    const drop = screen.getByRole("button", { name: "丢弃 internal/provider/retry.go 的改动" });
    await userEvent.click(drop);
    expect(discard).not.toHaveBeenCalled();
    expect(drop.textContent).toBe("确认丢弃");
    await userEvent.click(drop);
    expect(discard).toHaveBeenCalledWith({ path: "internal/provider/retry.go" });
    await userEvent.click(screen.getAllByRole("button", { name: "打开文件" })[0]);
    expect(onOpen).toHaveBeenCalledWith("internal/config/credentials.go");
  });
});

describe("the changes page with many files, as the WB-Changes-Many board draws it", () => {
  const many: WorkspaceChange[] = [
    { path: "internal/config/credentials.go", status: "M", unstaged: true, insertions: 12, deletions: 4 },
    { path: "internal/provider/openai/chunk_decoder.go", status: "A", staged: true, insertions: 42 },
    ...Array.from({ length: 8 }, (_, i) => ({ path: `bench/__pycache__/m${i}.pyc`, status: "??", unstaged: true }) as WorkspaceChange),
  ];

  it("puts what a new or untracked file is into its own header instead of a row under it", async () => {
    draw({ changes: many });
    await waitFor(() => expect(document.querySelectorAll(".wb-diff")).toHaveLength(1));
    expect(document.querySelectorAll(".wb-change-peek")).toHaveLength(0);
    expect([...document.querySelectorAll(".wb-change-tag")].map((e) => e.textContent)).toEqual(["新增", ...Array(8).fill("未跟踪")]);
  });

  it("says in the top line how many files are modified and how many are untracked", () => {
    draw({ changes: many });
    expect(document.querySelector(".wb-changes-head")!.textContent).toContain("10 个文件 已修改 2　未跟踪 8");
  });

  it("keeps the folded note under each file while there are few", () => {
    draw();
    expect(document.querySelectorAll(".wb-change-peek")).toHaveLength(1);
    expect(document.querySelectorAll(".wb-change-tag")).toHaveLength(0);
  });
});

describe("the explorer's changes section", () => {
  it("opens the changes page from its title and folds its rows from its arrow", async () => {
    const { WorkbenchChanges } = await import("./WorkbenchChanges");
    const onOpenPage = vi.fn();
    render(<WorkbenchChanges port={new MockPort()} changes={changes} current="" onOpen={vi.fn()} onChanged={vi.fn()} commit={{ button: null, card: null }} onOpenPage={onOpenPage} />);
    await userEvent.click(screen.getByRole("button", { name: "改动" }));
    expect(onOpenPage).toHaveBeenCalled();
    expect(document.querySelectorAll(".workbench-change").length).toBeGreaterThan(0);
    await userEvent.click(screen.getByRole("button", { name: "收起改动" }));
    expect(document.querySelectorAll(".workbench-change")).toHaveLength(0);
  });
});

it("gives the full branch name and the full count as hover text, since the header truncates them", () => {
  const port = new MockPort();
  render(<WorkbenchChangesPage port={port} branch="docs/memory-index-not-in-prefix" changes={[{ path: "a.go", status: "M", added: 3, removed: 1 } as never]} shown remote onOpen={() => {}} />);
  const head = document.querySelector(".wb-changes-head")!;
  expect(head.querySelector("b")?.getAttribute("title")).toBe("docs/memory-index-not-in-prefix");
  const count = head.querySelector("span")!;
  expect(count.getAttribute("title")).toBe(count.textContent!.replace(/\s+/g, " ").trim());
});
