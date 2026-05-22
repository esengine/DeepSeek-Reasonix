import { describe, expect, it, vi } from "vitest";
import { DeepSeekClient } from "../src/client.js";
import { CacheFirstLoop } from "../src/loop.js";
import { ImmutablePrefix } from "../src/memory/runtime.js";
import type { ChatMessage, ToolSpec } from "../src/types.js";

interface CapturedRequest {
  model: string;
  messages: ChatMessage[];
  tools: ToolSpec[] | undefined;
  thinking: string | undefined;
  body: Record<string, unknown>;
}

function fakeFetch(captured: CapturedRequest[], stubContent: string): typeof fetch {
  return vi.fn(async (_url: unknown, init: { body?: string } | undefined) => {
    const body = init?.body ? (JSON.parse(init.body) as Record<string, unknown>) : {};
    const messages = (body.messages ?? []) as ChatMessage[];
    const tools = body.tools as ToolSpec[] | undefined;
    const extra = body.extra_body as { thinking?: { type?: string } } | undefined;
    captured.push({
      model: body.model as string,
      messages,
      tools,
      thinking: extra?.thinking?.type,
      body,
    });
    return new Response(
      JSON.stringify({
        choices: [
          {
            index: 0,
            message: { role: "assistant", content: stubContent },
            finish_reason: "stop",
          },
        ],
        usage: { prompt_tokens: 100, completion_tokens: 20, total_tokens: 120 },
      }),
      { status: 200, headers: { "Content-Type": "application/json" } },
    );
  }) as unknown as typeof fetch;
}

const SYSTEM_PROMPT =
  "You are a coding agent for project X.\nFollow the user's instructions.\nUse tools as needed.";

const TOOLS: ToolSpec[] = [
  {
    type: "function",
    function: {
      name: "Read",
      description: "Read a file",
      parameters: {
        type: "object",
        properties: { path: { type: "string" } },
        required: ["path"],
      },
    },
  },
  {
    type: "function",
    function: {
      name: "Bash",
      description: "Run a shell command",
      parameters: {
        type: "object",
        properties: { command: { type: "string" } },
        required: ["command"],
      },
    },
  },
];

function seedTurns(loop: CacheFirstLoop, n: number, padding = 8): void {
  for (let i = 0; i < n; i++) {
    loop.log.append({
      role: "user",
      content: `q${i}: ${"context padding to weigh the turn ".repeat(padding)}`,
    });
    loop.log.append({
      role: "assistant",
      content: `a${i}: ${"reply padding to weigh the turn ".repeat(padding)}`,
    });
  }
}

describe("ContextManager fold sends cache-aligned summary request", () => {
  it("summary request reuses the main agent's system prompt verbatim", async () => {
    const captured: CapturedRequest[] = [];
    const client = new DeepSeekClient({
      apiKey: "sk-test",
      fetch: fakeFetch(captured, "compact prose summary."),
    });
    const loop = new CacheFirstLoop({
      client,
      prefix: new ImmutablePrefix({ system: SYSTEM_PROMPT, toolSpecs: TOOLS }),
      model: "deepseek-v4-flash",
      stream: false,
    });
    seedTurns(loop, 8);

    const result = await loop.compactHistory({ keepRecentTokens: 40 });
    expect(result.folded).toBe(true);
    expect(captured).toHaveLength(1);

    const req = captured[0]!;
    expect(req.messages[0]).toEqual({ role: "system", content: SYSTEM_PROMPT });
  });

  it("summary request reuses the main agent's tool list byte-for-byte", async () => {
    const captured: CapturedRequest[] = [];
    const client = new DeepSeekClient({
      apiKey: "sk-test",
      fetch: fakeFetch(captured, "summary."),
    });
    const loop = new CacheFirstLoop({
      client,
      prefix: new ImmutablePrefix({ system: SYSTEM_PROMPT, toolSpecs: TOOLS }),
      model: "deepseek-v4-flash",
      stream: false,
    });
    seedTurns(loop, 8);

    await loop.compactHistory({ keepRecentTokens: 40 });
    const req = captured[0]!;

    expect(req.tools).toBeDefined();
    expect(req.tools).toEqual(TOOLS);
    expect(JSON.stringify(req.tools)).toBe(JSON.stringify(TOOLS));
  });

  it("summary request preserves the head conversation bytes (head messages unmodified)", async () => {
    const captured: CapturedRequest[] = [];
    const client = new DeepSeekClient({
      apiKey: "sk-test",
      fetch: fakeFetch(captured, "summary."),
    });
    const loop = new CacheFirstLoop({
      client,
      prefix: new ImmutablePrefix({ system: SYSTEM_PROMPT, toolSpecs: TOOLS }),
      model: "deepseek-v4-flash",
      stream: false,
    });
    seedTurns(loop, 8);
    const logBeforeFold = loop.log.toMessages();

    await loop.compactHistory({ keepRecentTokens: 40 });
    const req = captured[0]!;

    expect(req.messages[0]!.role).toBe("system");
    const trailing = req.messages[req.messages.length - 1]!;
    expect(trailing.role).toBe("user");
    expect(typeof trailing.content === "string" ? trailing.content : "").toMatch(/Summarize/);

    // Strip system head + trailing instruction; what remains must equal a prefix of the pre-fold log.
    const middle = req.messages.slice(1, -1);
    for (let i = 0; i < middle.length; i++) {
      expect(middle[i]).toEqual(logBeforeFold[i]);
    }
  });

  it("summary request omits reasoning to avoid burning thinking tokens on paraphrase", async () => {
    const captured: CapturedRequest[] = [];
    const client = new DeepSeekClient({
      apiKey: "sk-test",
      fetch: fakeFetch(captured, "summary."),
    });
    const loop = new CacheFirstLoop({
      client,
      prefix: new ImmutablePrefix({ system: SYSTEM_PROMPT, toolSpecs: TOOLS }),
      model: "deepseek-v4-flash",
      stream: false,
    });
    seedTurns(loop, 8);

    await loop.compactHistory({ keepRecentTokens: 40 });
    const req = captured[0]!;
    expect(req.thinking).toBe("disabled");
    expect(req.body.reasoning_effort).toBeUndefined();
  });

  it("summary request pins to flash even when the session model is pro", async () => {
    const captured: CapturedRequest[] = [];
    const client = new DeepSeekClient({
      apiKey: "sk-test",
      fetch: fakeFetch(captured, "summary."),
    });
    const loop = new CacheFirstLoop({
      client,
      prefix: new ImmutablePrefix({ system: SYSTEM_PROMPT, toolSpecs: TOOLS }),
      model: "deepseek-v4-pro",
      stream: false,
    });
    seedTurns(loop, 8);

    await loop.compactHistory({ keepRecentTokens: 40 });
    expect(captured[0]!.model).toBe("deepseek-v4-flash");
  });

  it("skill-pinned bodies are sent to summarizer verbatim (head bytes unchanged)", async () => {
    const captured: CapturedRequest[] = [];
    const client = new DeepSeekClient({
      apiKey: "sk-test",
      fetch: fakeFetch(captured, "summary."),
    });
    const loop = new CacheFirstLoop({
      client,
      prefix: new ImmutablePrefix({ system: SYSTEM_PROMPT, toolSpecs: TOOLS }),
      model: "deepseek-v4-flash",
      stream: false,
    });

    const skillBody =
      '<skill-pin name="explore">\n# Skill: explore\n\nStep 1. Read entrypoints.\nStep 2. Trace flow.\n</skill-pin>';
    loop.log.append({
      role: "assistant",
      content: null,
      tool_calls: [
        { id: "c1", type: "function", function: { name: "run_skill", arguments: "{}" } },
      ],
    });
    loop.log.append({ role: "tool", tool_call_id: "c1", content: skillBody });
    seedTurns(loop, 6);

    const result = await loop.compactHistory({ keepRecentTokens: 40 });
    expect(result.folded).toBe(true);

    const req = captured[0]!;
    const serialized = JSON.stringify(req.messages);
    expect(serialized).toContain("Step 1. Read entrypoints.");
    expect(serialized).toContain("Step 2. Trace flow.");
    expect(serialized).not.toContain("preserved separately, do not summarize");

    const trailing = req.messages[req.messages.length - 1]!;
    const instruction = typeof trailing.content === "string" ? trailing.content : "";
    expect(instruction).toMatch(/pinned verbatim/);
    expect(instruction).toContain('"explore"');
  });

  it("trailing instruction is the only message after the head — everything before is cache prefix", async () => {
    const captured: CapturedRequest[] = [];
    const client = new DeepSeekClient({
      apiKey: "sk-test",
      fetch: fakeFetch(captured, "summary."),
    });
    const loop = new CacheFirstLoop({
      client,
      prefix: new ImmutablePrefix({ system: SYSTEM_PROMPT, toolSpecs: TOOLS }),
      model: "deepseek-v4-flash",
      stream: false,
    });
    seedTurns(loop, 8);

    await loop.compactHistory({ keepRecentTokens: 40 });
    const req = captured[0]!;
    const last = req.messages[req.messages.length - 1]!;
    const secondLast = req.messages[req.messages.length - 2]!;

    expect(last.role).toBe("user");
    // The instruction sits adjacent to the original head's final message —
    // no separator / wrapper that would push the cache-miss boundary inward.
    expect(secondLast).toBeDefined();
    expect(secondLast.role === "assistant" || secondLast.role === "tool").toBe(true);
  });
});
