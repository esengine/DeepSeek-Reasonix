import { describe, expect, it } from "vitest";
import { DeepSeekClient } from "../src/client.js";
import { ContextManager } from "../src/context-manager.js";
import { AppendOnlyLog } from "../src/memory/runtime.js";
import { SessionStats } from "../src/telemetry/stats.js";

function makeManager(log: AppendOnlyLog): ContextManager {
  const client = new DeepSeekClient({ apiKey: "sk-test" });
  return new ContextManager({
    client,
    log,
    stats: new SessionStats(),
    sessionName: null,
    getAbortSignal: () => new AbortController().signal,
    getCurrentTurn: () => 1,
  });
}

describe("ContextManager.getLogTokens", () => {
  it("returns 0 for an empty log", () => {
    const log = new AppendOnlyLog();
    const mgr = makeManager(log);
    expect(mgr.getLogTokens()).toBe(0);
  });

  it("counts user + assistant message content", () => {
    const log = new AppendOnlyLog();
    log.append({ role: "user", content: "hello world" });
    log.append({ role: "assistant", content: "hi there" });
    const mgr = makeManager(log);
    const tokens = mgr.getLogTokens();
    expect(tokens).toBeGreaterThan(0);
  });

  it("counts tool result messages", () => {
    const log = new AppendOnlyLog();
    log.append({ role: "user", content: "read file" });
    log.append({
      role: "tool",
      name: "read_file",
      tool_call_id: "t1",
      content: "file contents here",
    });
    const mgr = makeManager(log);
    const tokens = mgr.getLogTokens();
    expect(tokens).toBeGreaterThan(0);
  });

  it("counts assistant tool_calls when present", () => {
    const log = new AppendOnlyLog();
    log.append({ role: "user", content: "do something" });
    log.append({
      role: "assistant",
      content: "",
      tool_calls: [{ id: "t1", type: "function", function: { name: "test", arguments: "{}" } }],
    });
    const mgr = makeManager(log);
    const tokens = mgr.getLogTokens();
    expect(tokens).toBeGreaterThan(0);
  });

  it("ignores empty assistant tool_calls array", () => {
    const log = new AppendOnlyLog();
    log.append({ role: "user", content: "hello" });
    log.append({ role: "assistant", content: "hi", tool_calls: [] });
    const mgr = makeManager(log);
    const tokensWithEmpty = mgr.getLogTokens();

    const log2 = new AppendOnlyLog();
    log2.append({ role: "user", content: "hello" });
    log2.append({ role: "assistant", content: "hi" });
    const mgr2 = makeManager(log2);
    const tokensWithout = mgr2.getLogTokens();

    expect(tokensWithEmpty).toBe(tokensWithout);
  });

  it("drops after compact to reflect smaller log", () => {
    const log = new AppendOnlyLog();
    for (let i = 0; i < 10; i++) {
      log.append({ role: "user", content: `question ${i}` });
      log.append({ role: "assistant", content: `answer ${i}` });
    }
    const mgr = makeManager(log);
    const before = mgr.getLogTokens();
    expect(before).toBeGreaterThan(0);

    // Simulate compact: keep only last 2 turns
    const all = log.toMessages();
    log.compactInPlace(all.slice(all.length - 4));
    const after = mgr.getLogTokens();
    expect(after).toBeLessThan(before);
  });
});
