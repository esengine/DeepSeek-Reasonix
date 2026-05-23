/** summarizeToolResult — pure function; per-tool-name + structured-payload branches. */

import { describe, expect, it } from "vitest";
import {
  extractToolExitCode,
  formatDuration,
  selectToolPreviewLines,
  summarizeToolResult,
} from "../src/cli/ui/tool-summary.js";

describe("summarizeToolResult — error envelopes", () => {
  it("flags ERROR:-prefixed text as a real error and strips the prefix", () => {
    const out = summarizeToolResult("anything", "ERROR: file not found");
    expect(out.isError).toBe(true);
    expect(out.summary).toBe("file not found");
  });

  it("treats structured {error:...} JSON as an error and shows tag + detail", () => {
    const out = summarizeToolResult("read_file", JSON.stringify({ error: "ENOENT: no such file" }));
    expect(out.isError).toBe(true);
    expect(out.summary).toMatch(/^ENOENT/);
  });

  it("recognizes Plan / Choice control-flow signals as NON-errors", () => {
    const cases = [
      "PlanProposedError",
      "PlanRevisionProposedError",
      "ChoiceRequestedError",
      "NeedsConfirmationError",
    ];
    for (const tag of cases) {
      const out = summarizeToolResult(
        "any_tool",
        JSON.stringify({ error: `${tag}: STOP — picker shown to user.` }),
      );
      expect(out.isError, tag).toBe(false);
      expect(out.summary, tag).toMatch(new RegExp(tag));
    }
  });

  it("falls back to the bare error tag when no detail follows the colon", () => {
    const out = summarizeToolResult("x", JSON.stringify({ error: "SomeError" }));
    expect(out.isError).toBe(true);
    expect(out.summary).toBe("SomeError");
  });

  it("handles step_completed payload as a non-error tick", () => {
    const out = summarizeToolResult(
      "mark_step_complete",
      JSON.stringify({ kind: "step_completed", stepId: "step-2", result: "wired middleware" }),
    );
    expect(out.isError).toBe(false);
    expect(out.summary).toMatch(/✓ step-2/);
  });
});

describe("summarizeToolResult — known tools", () => {
  it("read_file: shows first line + line count + size", () => {
    const content = "import { foo } from 'bar';\nexport function baz() {}\n";
    const out = summarizeToolResult("read_file", content);
    expect(out.isError).toBe(false);
    expect(out.summary).toMatch(/import.*foo/);
    expect(out.summary).toMatch(/3 lines/);
    expect(out.summary).toMatch(/B/);
  });

  it("list_directory: shows entry count", () => {
    const out = summarizeToolResult("list_directory", "src/\ntests/\nREADME.md\n");
    expect(out.summary).toBe("3 entries");
  });

  it("list_directory with one entry uses singular", () => {
    const out = summarizeToolResult("list_directory", "only-thing\n");
    expect(out.summary).toBe("1 entry");
  });

  it("search_content: shows match count + first match", () => {
    const out = summarizeToolResult(
      "search_content",
      "src/foo.ts:12: const x = 1\nsrc/bar.ts:34: const x = 2",
    );
    expect(out.summary).toMatch(/2 matches/);
    expect(out.summary).toMatch(/src\/foo\.ts/);
  });

  it("search_content: explicit no-match path", () => {
    const out = summarizeToolResult("search_content", "");
    expect(out.summary).toBe("no matches");
  });

  it("run_command: surfaces exit code and first line", () => {
    const out = summarizeToolResult("run_command", "exit 0\nhello world");
    expect(out.isError).toBe(false);
    expect(out.summary).toMatch(/exit 0/);
  });

  it("run_command: non-zero exit flags the row as an error", () => {
    const out = summarizeToolResult("run_command", "exit 1\nError: something went wrong");
    expect(out.isError).toBe(true);
    expect(out.summary).toMatch(/exit 1/);
  });

  it("write_file: shows wrote line count + size", () => {
    const out = summarizeToolResult("write_file", "alpha\nbeta\ngamma\n");
    expect(out.isError).toBe(false);
    expect(out.summary).toMatch(/wrote/);
    expect(out.summary).toMatch(/4 lines/);
  });

  it("MCP-bridged tools pick up the same summary via suffix match", () => {
    // `filesystem_read_file` should hit the read_file branch.
    const out = summarizeToolResult(
      "filesystem_read_file",
      "import { foo } from 'bar';\nexport function baz() {}\n",
    );
    expect(out.summary).toMatch(/lines/);
    expect(out.summary).toMatch(/import.*foo/);
  });

  it("suffix match doesn't false-trigger on non-underscore prefixes", () => {
    // `myread_file` (no underscore separator) should NOT match read_file.
    const out = summarizeToolResult("myread_file", "anything");
    expect(out.summary).not.toMatch(/lines/);
  });
});

describe("formatDuration", () => {
  it("renders sub-100ms in milliseconds", () => {
    expect(formatDuration(0)).toBe("0ms");
    expect(formatDuration(47)).toBe("47ms");
    expect(formatDuration(99)).toBe("99ms");
  });

  it("renders sub-second times in 1-decimal seconds", () => {
    expect(formatDuration(100)).toBe("0.1s");
    expect(formatDuration(450)).toBe("0.5s");
    expect(formatDuration(999)).toBe("1.0s");
  });

  it("renders sub-10s times with one decimal", () => {
    expect(formatDuration(1234)).toBe("1.2s");
    expect(formatDuration(8500)).toBe("8.5s");
  });

  it("renders 10s–60s as integer seconds", () => {
    expect(formatDuration(10_000)).toBe("10s");
    expect(formatDuration(45_900)).toBe("46s");
  });

  it("renders minutes-and-seconds for long runs", () => {
    expect(formatDuration(60_000)).toBe("1m");
    expect(formatDuration(90_000)).toBe("1m30s");
    expect(formatDuration(125_500)).toBe("2m6s");
  });

  it("returns empty string for invalid input", () => {
    expect(formatDuration(Number.NaN)).toBe("");
    expect(formatDuration(-1)).toBe("");
    expect(formatDuration(Number.POSITIVE_INFINITY)).toBe("");
  });
});

describe("summarizeToolResult — generic fallback", () => {
  it("returns first line only for short content", () => {
    const out = summarizeToolResult("custom_tool", "hello");
    expect(out.summary).toBe("hello");
    expect(out.isError).toBe(false);
  });

  it("appends a size hint for long content", () => {
    const long = `first line\n${"x".repeat(2000)}`;
    const out = summarizeToolResult("custom_tool", long);
    expect(out.summary).toMatch(/first line/);
    expect(out.summary).toMatch(/KB/);
  });

  it("handles empty string as (empty)", () => {
    const out = summarizeToolResult("anything", "");
    expect(out.summary).toBe("(empty)");
  });

  it("clips overly long single lines with an ellipsis and stays under the budget", () => {
    const out = summarizeToolResult("anything", "a".repeat(500));
    expect(out.summary).toMatch(/…/);
    expect(out.summary.length).toBeLessThanOrEqual(80);
  });
});

describe("extractToolExitCode", () => {
  it("parses shell result markers from run_command output", () => {
    expect(extractToolExitCode("run_command", "$ node test.mjs\n[exit 1]\nError")).toBe(1);
    expect(extractToolExitCode("run_command", "$ echo ok\n[exit 0]\nok")).toBe(0);
  });

  it("parses shell result markers from run_background output", () => {
    expect(extractToolExitCode("run_background", "$ npm test\n[exit 2]\nfailed")).toBe(2);
  });

  it("ignores non-shell tools and non-marker text", () => {
    expect(extractToolExitCode("read_file", "$ node test.mjs\n[exit 1]\nError")).toBeUndefined();
    expect(extractToolExitCode("run_command", "the docs mention [exit 1] inline")).toBeUndefined();
    expect(extractToolExitCode("run_command", "$ cmd\n[exit ?]\nunknown")).toBeUndefined();
  });
});

describe("selectToolPreviewLines", () => {
  const lineText = (rows: ReturnType<typeof selectToolPreviewLines>["rows"]): string[] =>
    rows.flatMap((row) => (row.kind === "line" ? [row.text] : []));

  it("pins Python unittest failure lines before noisy cleanup tail", () => {
    const output = [
      "$ python3 test_retry_policy.py",
      "[exit 1]",
      "test_exponential_backoff (__main__.RetryTests) ... FAIL",
      "AssertionError: 300 != 800 : third retry should wait 800ms",
      "cleanup: checked worker shard 1/30",
      "cleanup: checked worker shard 2/30",
      "cleanup: checked worker shard 3/30",
      "cleanup: checked worker shard 4/30",
      "cleanup: checked worker shard 5/30",
      "cleanup: checked worker shard 6/30",
    ].join("\n");

    const rows = selectToolPreviewLines({
      toolName: "run_command",
      output,
      exitCode: 1,
      tailLines: 2,
      verbose: false,
    }).rows;

    const lines = lineText(rows);
    expect(lines).toContain("test_exponential_backoff (__main__.RetryTests) ... FAIL");
    expect(lines).toContain("AssertionError: 300 != 800 : third retry should wait 800ms");
    expect(lines.at(-2)).toBe("cleanup: checked worker shard 5/30");
    expect(lines.at(-1)).toBe("cleanup: checked worker shard 6/30");
    expect(rows.some((row) => row.kind === "hidden")).toBe(true);
  });

  it("pins Node assertion mismatch lines instead of showing only the process tail", () => {
    const output = [
      "$ node test.mjs",
      "[exit 1]",
      "AssertionError [ERR_ASSERTION]: VIP25 should reduce cart",
      "actual: 10200",
      "expected: 9000",
      "operator: strictEqual",
      "}",
      "Node.js v22.22.0",
    ].join("\n");

    const rows = selectToolPreviewLines({
      toolName: "run_command",
      output,
      exitCode: 1,
      tailLines: 2,
      verbose: false,
    }).rows;

    const lines = lineText(rows);
    expect(lines).toContain("AssertionError [ERR_ASSERTION]: VIP25 should reduce cart");
    expect(lines).toContain("actual: 10200");
    expect(lines).toContain("expected: 9000");
    expect(lines.at(-1)).toBe("Node.js v22.22.0");
  });

  it("keeps successful command previews on the ordinary tail path", () => {
    const output = [
      "$ node test.mjs",
      "[exit 0]",
      "AssertionError string in a successful fixture should not pin",
      "line 1",
      "line 2",
      "line 3",
    ].join("\n");

    const rows = selectToolPreviewLines({
      toolName: "run_command",
      output,
      exitCode: 0,
      tailLines: 2,
      verbose: false,
    }).rows;

    expect(lineText(rows)).toEqual(["line 2", "line 3"]);
  });

  it("keeps non-shell tools on the ordinary tail path", () => {
    const output = "AssertionError: hidden\nmiddle\nlast";
    const rows = selectToolPreviewLines({
      toolName: "read_file",
      output,
      exitCode: 1,
      tailLines: 1,
      verbose: false,
    }).rows;

    expect(lineText(rows)).toEqual(["last"]);
  });
});
