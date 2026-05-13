import { mkdtempSync, readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
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

describe("acp --transcript", () => {
  let tmpDir: string;

  beforeAll(() => {
    tmpDir = mkdtempSync(join(tmpdir(), "reasonix-acp-transcript-"));
  });

  afterAll(() => {
    rmSync(tmpDir, { recursive: true, force: true });
  });

  it("writes _meta with source 'reasonix acp' and records the ACP session sequence", async () => {
    const path = join(tmpDir, "session.jsonl");
    const stream = openTranscriptFile(path, {
      version: 1,
      source: "reasonix acp",
      model: "deepseek-chat",
      startedAt: "2026-05-13T00:00:00Z",
    });

    const ctx = { model: "deepseek-chat", prefixHash: "acp-prefix-hash" };
    const stats = new SessionStats();
    const usage = new Usage(1200, 80, 1280, 1100, 100);
    const turnStats = stats.record(1, "deepseek-chat", usage);

    const events: LoopEvent[] = [
      { turn: 1, role: "assistant_delta", content: "Writing" },
      { turn: 1, role: "assistant_delta", content: " the file." },
      {
        turn: 1,
        role: "tool",
        content: "wrote 9 chars to /tmp/x",
        toolName: "write_file",
        toolArgs: '{"path":"/tmp/x","content":"ACP WORKS"}',
      },
      { turn: 1, role: "assistant_final", content: "Done.", stats: turnStats },
    ];

    for (const ev of events) {
      writeRecord(stream, recordFromLoopEvent(ev, ctx));
    }
    await new Promise<void>((resolve) => stream.end(resolve));

    const { meta, records } = parseTranscript(readFileSync(path, "utf8"));

    expect(meta).not.toBeNull();
    expect(meta?.source).toBe("reasonix acp");
    expect(meta?.version).toBe(1);
    expect(meta?.model).toBe("deepseek-chat");

    expect(records).toHaveLength(4);
    expect(records.map((r) => r.role)).toEqual([
      "assistant_delta",
      "assistant_delta",
      "tool",
      "assistant_final",
    ]);

    const toolRec = records.find((r) => r.role === "tool");
    expect(toolRec?.tool).toBe("write_file");
    expect(toolRec?.args).toBe('{"path":"/tmp/x","content":"ACP WORKS"}');

    const finalRec = records.find((r) => r.role === "assistant_final");
    expect(finalRec?.usage?.total_tokens).toBe(1280);
    expect(finalRec?.usage?.prompt_cache_hit_tokens).toBe(1100);
    expect(finalRec?.cost).toBeGreaterThan(0);
    expect(finalRec?.model).toBe("deepseek-chat");
    expect(finalRec?.prefixHash).toBe("acp-prefix-hash");
  });

  it("appends records across multiple session/prompt turns in order", async () => {
    const path = join(tmpDir, "multi-turn.jsonl");
    const stream = openTranscriptFile(path, {
      version: 1,
      source: "reasonix acp",
      startedAt: "2026-05-13T00:00:00Z",
    });

    const ctx = { model: "deepseek-chat", prefixHash: "h" };
    const turn1: LoopEvent = { turn: 1, role: "assistant_final", content: "first" };
    const turn2: LoopEvent = { turn: 2, role: "assistant_final", content: "second" };

    writeRecord(stream, recordFromLoopEvent(turn1, ctx));
    writeRecord(stream, recordFromLoopEvent(turn2, ctx));
    await new Promise<void>((resolve) => stream.end(resolve));

    const { records } = parseTranscript(readFileSync(path, "utf8"));
    expect(records.map((r) => r.turn)).toEqual([1, 2]);
    expect(records.map((r) => r.content)).toEqual(["first", "second"]);
  });
});

describe("acp-driver.ndjson fixture", () => {
  it("is valid NDJSON and drives the verified happy path (initialize → session/new → session/prompt)", () => {
    const here = dirname(fileURLToPath(import.meta.url));
    const raw = readFileSync(join(here, "fixtures", "acp-driver.ndjson"), "utf8");
    const lines = raw.split("\n").filter((l) => l.trim().length > 0);
    const requests = lines.map((l) => JSON.parse(l));

    expect(requests).toHaveLength(3);
    expect(requests.map((r) => r.method)).toEqual(["initialize", "session/new", "session/prompt"]);
    for (const r of requests) {
      expect(r.jsonrpc).toBe("2.0");
      expect(typeof r.id).toBe("number");
    }
    expect(requests[0].params.protocolVersion).toBe(1);
    expect(requests[2].params.prompt[0].type).toBe("text");
    // sessionId is a placeholder by design — a real smoke test must splice the value
    // from the session/new response before sending request #3, or the agent returns
    // ERR_INVALID_PARAMS "unknown session" with no actionable hint.
    expect(requests[2].params.sessionId).toMatch(/^<.+>$/);
  });
});
