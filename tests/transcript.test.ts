import { mkdtempSync, readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterAll, beforeAll, describe, expect, it } from "vitest";
import { Usage } from "../src/client.js";
import type { LoopEvent } from "../src/loop.js";
import { SessionStats } from "../src/telemetry/stats.js";
import {
  openTranscriptFile,
  parseTranscript,
  recordFromLoopEvent,
  writeRecord,
} from "../src/transcript/log.js";

describe("transcript writer / reader round-trip", () => {
  let tmpDir: string;

  beforeAll(() => {
    tmpDir = mkdtempSync(join(tmpdir(), "reasonix-test-"));
  });

  afterAll(() => {
    rmSync(tmpDir, { recursive: true, force: true });
  });

  it("writes meta + assistant_final + tool records and reads them back", async () => {
    const path = join(tmpDir, "round-trip.jsonl");
    const stream = openTranscriptFile(path, {
      version: 1,
      source: "test",
      model: "deepseek-chat",
      task: "t01",
      mode: "reasonix",
      repeat: 1,
      startedAt: "2026-04-21T00:00:00Z",
    });

    // Build a realistic assistant_final event using SessionStats.
    const stats = new SessionStats();
    const usage = new Usage(1000, 100, 1100, 800, 200);
    const turnStats = stats.record(1, "deepseek-chat", usage);

    const assistantEv: LoopEvent = {
      turn: 1,
      role: "assistant_final",
      content: "Hello world.",
      stats: turnStats,
    };
    const toolEv: LoopEvent = {
      turn: 1,
      role: "tool",
      content: '{"ok":true}',
      toolName: "add",
      toolArgs: '{"a":1,"b":2}',
    };

    writeRecord(
      stream,
      recordFromLoopEvent(assistantEv, { model: "deepseek-chat", prefixHash: "abc123def456" }),
    );
    writeRecord(
      stream,
      recordFromLoopEvent(toolEv, { model: "deepseek-chat", prefixHash: "abc123def456" }),
    );
    await new Promise<void>((resolve) => stream.end(resolve));

    const raw = readFileSync(path, "utf8");
    const { meta, records } = parseTranscript(raw);

    expect(meta).not.toBeNull();
    expect(meta?.source).toBe("test");
    expect(meta?.task).toBe("t01");

    expect(records).toHaveLength(2);
    const a = records[0]!;
    expect(a.role).toBe("assistant_final");
    expect(a.content).toBe("Hello world.");
    expect(a.usage?.prompt_cache_hit_tokens).toBe(800);
    expect(a.cost).toBeGreaterThan(0);
    expect(a.model).toBe("deepseek-chat");
    expect(a.prefixHash).toBe("abc123def456");

    const t = records[1]!;
    expect(t.role).toBe("tool");
    expect(t.tool).toBe("add");
    expect(t.args).toBe('{"a":1,"b":2}');
  });

  it("parses a v0.1-shape transcript (no usage / cost / prefixHash) without losing records", () => {
    // Old format — just ts/turn/role/content/tool. No optional fields.
    const raw = [
      `{"ts":"2026-04-21T00:00:00Z","turn":1,"role":"user","content":"hi"}`,
      `{"ts":"2026-04-21T00:00:01Z","turn":1,"role":"assistant_final","content":"hello"}`,
    ].join("\n");
    const { meta, records } = parseTranscript(raw);
    expect(meta).toBeNull();
    expect(records).toHaveLength(2);
    expect(records[0]!.role).toBe("user");
    expect(records[1]!.role).toBe("assistant_final");
    expect(records[1]!.usage).toBeUndefined();
    expect(records[1]!.cost).toBeUndefined();
  });

  it("skips malformed lines silently (partial writes shouldn't crash replay)", () => {
    const raw = [
      `{"ts":"2026-04-21T00:00:00Z","turn":1,"role":"user","content":"hi"}`,
      "{not valid json",
      `{"wrong":"shape"}`,
      `{"ts":"2026-04-21T00:00:01Z","turn":1,"role":"assistant_final","content":"yo"}`,
    ].join("\n");
    const { records } = parseTranscript(raw);
    expect(records).toHaveLength(2);
  });
});
