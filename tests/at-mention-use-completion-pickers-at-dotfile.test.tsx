/**
 * Tests for @-mention search-mode directory-prefix filtering in
 * `useCompletionPickers`. Ensures that when the user types a path
 * like `@subdir/.gitignore`, only files under `subdir/` appear —
 * a same-name file at the project root is excluded from the list.
 */
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { Box, Text } from "ink";
import React, { useState } from "react";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { useCompletionPickers } from "../src/cli/ui/useCompletionPickers.js";
import { render } from "./helpers/ink-test.js";

/** Wraps `useCompletionPickers` in a renderable Ink component and
 *  surfaces the picker state as plain text so we can snapshot it. */
function Harness({ rootDir, initialInput }: { rootDir: string; initialInput?: string }) {
  const [input, setInput] = useState(initialInput ?? "");
  const pickers = useCompletionPickers({
    input,
    setInput,
    codeMode: { rootDir },
    rootDir,
    models: null,
    mcpServers: [],
  });

  if (!pickers.atState) return <Text>no-picker</Text>;

  const { atState, atSelected } = pickers;
  const entries = atState.entries;
  if (entries.length === 0) return <Text>empty</Text>;
  return (
    <Box flexDirection="column">
      <Text>
        {atState.kind}: {atSelected}
      </Text>
      {entries.map((e, i) => (
        <Text key={`${e.insertPath}:${i}`}>
          {i === atSelected ? "> " : "  "}
          {e.insertPath}
          {e.isDir ? "/" : ""}
        </Text>
      ))}
    </Box>
  );
}

/** Poll `lastFrame()` until it contains `needle` or `timeoutMs` elapses.
 *  Poll interval starts at 10 ms and doubles up to 200 ms to stay responsive
 *  on fast machines without hammering slow CI runners. */
async function pollFrame(
  lastFrame: () => string | undefined,
  needle: string,
  timeoutMs = 5000,
): Promise<string> {
  const deadline = Date.now() + timeoutMs;
  let interval = 10;
  let frame = lastFrame();
  while (!frame?.includes(needle) && Date.now() < deadline) {
    await new Promise((r) => setTimeout(r, interval));
    interval = Math.min(interval * 2, 200);
    frame = lastFrame();
  }
  if (!frame) throw new Error("no frame produced");
  return frame;
}

describe("useCompletionPickers @-mention directory-prefix filtering", () => {
  let root: string;

  beforeEach(() => {
    root = mkdtempSync(join(tmpdir(), "reasonix-at-picker-"));
    // Reproduce the exact scenario from the bug report:
    //   root/.gitignore  (same-name file at project root)
    //   root/benchmarks/compression-eval/.gitignore  (the one the user wants)
    //   root/benchmarks/compression-eval/other.ts    (another file in the same dir)
    mkdirSync(join(root, "benchmarks", "compression-eval"), { recursive: true });
    writeFileSync(join(root, ".gitignore"), "# root\n");
    writeFileSync(join(root, "benchmarks", "compression-eval", ".gitignore"), "# sub\n");
    writeFileSync(join(root, "benchmarks", "compression-eval", "other.ts"), "");
  });

  afterEach(() => {
    rmSync(root, { recursive: true, force: true });
  });

  it("filters search results to only files under the typed directory prefix", async () => {
    // Simulate typing `@benchmarks/compression-eval/.gitignore`
    const { lastFrame, unmount } = render(
      <Harness rootDir={root} initialInput="@benchmarks/compression-eval/.gitignore" />,
      { stdout: process.stdout as never },
    );

    // Search mode is async (file walker + debounce). Poll until the subdir
    // entry arrives — that means both the walk and the filter ran.
    const frame = await pollFrame(lastFrame, "compression-eval/.gitignore");

    // Should show search mode with only the subdirectory entry.
    // The root `.gitignore` MUST NOT appear as a bare entry.
    expect(frame).toContain("search:");
    // A line that is just `.gitignore` without a directory prefix is the root file.
    expect(frame).not.toMatch(/^ {2}\.gitignore$/m);
    unmount();
  });

  it("at root, an unqualified dotfile shows both entries (no dir prefix to filter by)", async () => {
    // Simulate typing `@.gitignore` — no directory prefix.
    const { lastFrame, unmount } = render(<Harness rootDir={root} initialInput="@.gitignore" />, {
      stdout: process.stdout as never,
    });

    // Poll until the subdir entry appears — signals the walk completed.
    const frame = await pollFrame(lastFrame, "compression-eval/.gitignore");

    // Both entries should appear because dir is empty (root level).
    expect(frame).toContain(".gitignore");
    expect(frame).toContain("benchmarks/compression-eval/.gitignore");
    unmount();
  });

  it("browse mode shows all entries in the directory (no filtering needed)", async () => {
    // Simulate browsing into `benchmarks/compression-eval/`
    const { lastFrame, unmount } = render(
      <Harness rootDir={root} initialInput="@benchmarks/compression-eval/" />,
      { stdout: process.stdout as never },
    );

    // Browse mode calls `listDirectory` asynchronously. Poll until a file
    // entry appears — `other.ts` is only present once the listing resolves.
    // (Polling for `browse:` would return immediately with only the parent
    // entry, failing on slow CI machines.)
    const frame = await pollFrame(lastFrame, "other.ts");

    expect(frame).toContain("browse:");
    expect(frame).toContain(".gitignore");
    expect(frame).toContain("other.ts");
    expect(frame).not.toMatch(/^\s+.gitignore\n/m);
    unmount();
  });
});
