import { existsSync, mkdtempSync, rmSync } from "node:fs";
import { homedir, tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { DeepSeekClient } from "../src/client.js";
import { CacheFirstLoop } from "../src/loop.js";
import { ImmutablePrefix } from "../src/memory/runtime.js";
import { loadSessionMessages } from "../src/memory/session.js";
import type { ChatMessage } from "../src/types.js";

describe("loop persists user message at step entry (issue #943)", () => {
  let tmp: string;

  beforeEach(() => {
    tmp = mkdtempSync(join(tmpdir(), "reasonix-loop943-"));
    vi.stubEnv("USERPROFILE", tmp);
    vi.stubEnv("HOME", tmp);
    vi.spyOn(require("node:os"), "homedir").mockReturnValue(tmp);
  });

  afterEach(() => {
    vi.restoreAllMocks();
    vi.unstubAllEnvs();
    if (existsSync(tmp)) rmSync(tmp, { recursive: true, force: true });
  });

  it("writes the user message to the session log before the first API call settles, so a mid-stream abort doesn't drop it", async () => {
    // Fake fetch that never resolves on its own — only the abort signal
    // terminates it. Simulates the desktop user clicking a different
    // session while the AI is still streaming.
    const client = new DeepSeekClient({
      apiKey: "sk-test",
      fetch: vi.fn(async (_url: unknown, init: { signal?: AbortSignal } | undefined) => {
        const signal = init?.signal;
        return new Promise<Response>((_resolve, reject) => {
          signal?.addEventListener("abort", () =>
            reject(new DOMException("This operation was aborted", "AbortError")),
          );
        });
      }) as unknown as typeof fetch,
      retry: { maxAttempts: 1 },
    });

    const sessionName = "bug943-pre-response-abort";
    const loop = new CacheFirstLoop({
      client,
      prefix: new ImmutablePrefix({ system: "s" }),
      stream: false,
      session: sessionName,
    });

    const consumed = (async () => {
      for await (const ev of loop.step("hello — switching away soon")) {
        if (ev.role === "done") break;
      }
    })();

    // Yield a tick so step() reaches the awaited fetch, then abort to
    // simulate the session-switch tear-down.
    await new Promise((r) => setTimeout(r, 10));
    loop.abort();
    await consumed;

    // The session JSONL must exist on disk and contain the user prompt.
    // Pre-fix this file would be missing entirely, making the session
    // invisible to the sidebar's `listSessions()` glob.
    const persisted = loadSessionMessages(sessionName);
    const firstUser = persisted.find((m) => m.role === "user");
    expect(firstUser).toBeDefined();
    expect(firstUser?.content).toBe("hello — switching away soon");
  });

  it("writes the user message immediately even when the API call succeeds normally", async () => {
    const client = new DeepSeekClient({
      apiKey: "sk-test",
      fetch: vi.fn(async () => {
        return new Response(
          JSON.stringify({
            choices: [
              {
                index: 0,
                message: { role: "assistant", content: "ok" },
                finish_reason: "stop",
              },
            ],
            usage: { prompt_tokens: 1, completion_tokens: 1, total_tokens: 2 },
          }),
          { status: 200, headers: { "Content-Type": "application/json" } },
        );
      }) as unknown as typeof fetch,
    });

    const sessionName = "bug943-happy-path";
    const loop = new CacheFirstLoop({
      client,
      prefix: new ImmutablePrefix({ system: "s" }),
      stream: false,
      session: sessionName,
    });

    for await (const ev of loop.step("happy path")) {
      if (ev.role === "done") break;
    }

    const persisted = loadSessionMessages(sessionName);
    // First entry is the user message; followed by assistant.
    expect(persisted[0]).toEqual({ role: "user", content: "happy path" });
    expect(persisted.length).toBeGreaterThanOrEqual(2);
    expect(persisted.some((m) => m.role === "assistant")).toBe(true);
    // No duplicate user copies.
    const userMsgs = persisted.filter((m) => m.role === "user");
    expect(userMsgs).toHaveLength(1);
  });

  it("persists send-time healing of dangling tool_calls so the session does not stay poisoned", async () => {
    const requestMessages: ChatMessage[][] = [];
    const client = new DeepSeekClient({
      apiKey: "sk-test",
      fetch: vi.fn(async (_url: unknown, init: { body?: string } | undefined) => {
        const body = init?.body ? JSON.parse(init.body) : {};
        requestMessages.push(body.messages as ChatMessage[]);
        return new Response(
          JSON.stringify({
            choices: [
              {
                index: 0,
                message: { role: "assistant", content: "recovered" },
                finish_reason: "stop",
              },
            ],
            usage: { prompt_tokens: 1, completion_tokens: 1, total_tokens: 2 },
          }),
          { status: 200, headers: { "Content-Type": "application/json" } },
        );
      }) as unknown as typeof fetch,
    });

    const sessionName = "issue1079-dangling-tool-heal";
    const loop = new CacheFirstLoop({
      client,
      prefix: new ImmutablePrefix({ system: "s" }),
      stream: false,
      session: sessionName,
    });
    loop.appendAndPersist({ role: "user", content: "before sleep" });
    loop.appendAndPersist({
      role: "assistant",
      content: "",
      tool_calls: [{ id: "lost", type: "function", function: { name: "read", arguments: "{}" } }],
    });

    await loop.run("continue after wake");

    expect(
      requestMessages[0]!.some((m) => m.role === "assistant" && (m.tool_calls?.length ?? 0) > 0),
    ).toBe(false);
    expect(
      loop.log.entries.some((m) => m.role === "assistant" && (m.tool_calls?.length ?? 0) > 0),
    ).toBe(false);
    expect(
      loadSessionMessages(sessionName).some(
        (m) => m.role === "assistant" && (m.tool_calls?.length ?? 0) > 0,
      ),
    ).toBe(false);
  });
});
