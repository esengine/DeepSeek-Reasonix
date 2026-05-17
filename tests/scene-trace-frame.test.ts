import { describe, expect, it } from "vitest";
import {
  parseRecentCards,
  parseSessions,
  parseSlashMatches,
  summarizeCard,
  toSceneCard,
} from "../src/cli/ui/hooks/useSceneTrace.js";
import type { Card } from "../src/cli/ui/state/cards.js";

function userCard(text: string): Card {
  return { id: "u1", ts: 0, kind: "user", text };
}

function toolCard(name: string, done: boolean): Card {
  return {
    id: "t1",
    ts: 0,
    kind: "tool",
    name,
    args: {},
    output: "",
    done,
    elapsedMs: 0,
  };
}

describe("summarizeCard", () => {
  it("returns the first line for a user card", () => {
    expect(summarizeCard(userCard("hello\nworld"))).toBe("hello");
  });

  it("clips long first lines and appends an ellipsis", () => {
    const s = summarizeCard(userCard("x".repeat(200)));
    expect(s).toHaveLength(70);
    expect(s?.endsWith("…")).toBe(true);
  });

  it("returns the tool name with a running marker when not done", () => {
    expect(summarizeCard(toolCard("bash", false))).toBe("bash …");
  });

  it("returns the tool name plain when done", () => {
    expect(summarizeCard(toolCard("bash", true))).toBe("bash");
  });

  it("returns undefined for no card", () => {
    expect(summarizeCard(undefined)).toBeUndefined();
  });
});

describe("toSceneCard — wire-format serializer", () => {
  it("returns kind + summary + body + ts for user cards", () => {
    expect(toSceneCard({ id: "u1", ts: 42, kind: "user", text: "hi\nthere" })).toEqual({
      kind: "user",
      summary: "hi",
      body: "hi\nthere",
      ts: 42,
    });
  });

  it("includes args + status + elapsed + id for a completed tool card", () => {
    const card = toSceneCard({
      id: "tool-abc123def4",
      ts: 0,
      kind: "tool",
      name: "Read",
      args: { path: "src/parser.ts" },
      output: "",
      done: true,
      exitCode: 0,
      elapsedMs: 120,
    });
    expect(card).toEqual({
      kind: "tool",
      summary: "Read",
      args: "src/parser.ts",
      status: "ok",
      elapsed: "120ms",
      id: "#def4",
    });
  });

  it("marks running tool cards with status=running and omits elapsed", () => {
    const card = toSceneCard({
      id: "t1",
      ts: 0,
      kind: "tool",
      name: "Bash",
      args: { command: "pnpm test" },
      output: "",
      done: false,
      elapsedMs: 0,
    });
    expect(card.status).toBe("running");
    expect(card.args).toBe("pnpm test");
    expect(card.elapsed).toBeUndefined();
  });

  it("marks rejected / non-zero exit tool cards as status=err", () => {
    const failed = toSceneCard({
      id: "t1",
      ts: 0,
      kind: "tool",
      name: "Bash",
      args: { command: "false" },
      output: "",
      done: true,
      exitCode: 1,
      elapsedMs: 10,
    });
    expect(failed.status).toBe("err");

    const rejected = toSceneCard({
      id: "t1",
      ts: 0,
      kind: "tool",
      name: "Bash",
      args: {},
      output: "",
      done: true,
      elapsedMs: 5,
      rejected: true,
    });
    expect(rejected.status).toBe("err");
  });

  it("formats elapsed >= 1000ms in seconds with 2 decimals", () => {
    const c = toSceneCard({
      id: "t",
      ts: 0,
      kind: "tool",
      name: "x",
      args: {},
      output: "",
      done: true,
      elapsedMs: 2123,
    });
    expect(c.elapsed).toBe("2.12s");
  });

  it("extracts the primary arg by common-key preference (path > pattern > etc.)", () => {
    const withPath = toSceneCard({
      id: "t",
      ts: 0,
      kind: "tool",
      name: "x",
      args: { foo: "bar", path: "src/x.ts" },
      output: "",
      done: true,
      elapsedMs: 0,
    });
    expect(withPath.args).toBe("src/x.ts");
    const withPattern = toSceneCard({
      id: "t",
      ts: 0,
      kind: "tool",
      name: "x",
      args: { pattern: "foo", in: "src/" },
      output: "",
      done: true,
      elapsedMs: 0,
    });
    expect(withPattern.args).toBe("foo");
  });

  it("returns reasoning cards with paragraphs/elapsed meta + raw body", () => {
    const c = toSceneCard({
      id: "r1",
      ts: 1000,
      kind: "reasoning",
      text: "step one\nstep two",
      paragraphs: 2,
      tokens: 100,
      streaming: false,
      endedAt: 4500,
    });
    expect(c.kind).toBe("reasoning");
    expect(c.body).toBe("step one\nstep two");
    expect(c.ts).toBe(1000);
    expect(c.meta).toBe("2 steps · 3.50s");
  });

  it("returns streaming cards with done/streaming meta", () => {
    const running = toSceneCard({
      id: "s1",
      ts: 0,
      kind: "streaming",
      text: "hi",
      done: false,
    });
    expect(running.meta).toBe("streaming…");

    const done = toSceneCard({
      id: "s2",
      ts: 0,
      kind: "streaming",
      text: "hi",
      done: true,
    });
    expect(done.meta).toBe("done");
  });

  it("falls back to plain { kind, summary } for unhandled kinds", () => {
    const card = toSceneCard({
      id: "d1",
      ts: 0,
      kind: "diff",
      file: "src/x.ts",
      hunks: [],
      adds: 0,
      dels: 0,
    } as Card);
    expect(card.kind).toBe("diff");
    expect(card.summary).toBe("src/x.ts");
    expect(card.body).toBeUndefined();
    expect(card.ts).toBeUndefined();
  });
});

describe("parseRecentCards", () => {
  it("returns [] for undefined / empty / malformed input", () => {
    expect(parseRecentCards(undefined)).toEqual([]);
    expect(parseRecentCards("")).toEqual([]);
    expect(parseRecentCards("not-json")).toEqual([]);
    expect(parseRecentCards('{"not":"array"}')).toEqual([]);
  });

  it("decodes a JSON array of card payloads with optional fields", () => {
    const json = JSON.stringify([
      { kind: "user", summary: "hi", body: "hi", ts: 1 },
      {
        kind: "tool",
        summary: "Read",
        args: "src/x.ts",
        status: "ok",
        elapsed: "12ms",
        id: "#abc",
      },
    ]);
    expect(parseRecentCards(json)).toEqual([
      { kind: "user", summary: "hi", body: "hi", ts: 1 },
      {
        kind: "tool",
        summary: "Read",
        args: "src/x.ts",
        status: "ok",
        elapsed: "12ms",
        id: "#abc",
      },
    ]);
  });

  it("drops entries missing kind or summary", () => {
    const json = JSON.stringify([
      { kind: "user", summary: "ok" },
      { kind: "tool" },
      { summary: "no-kind" },
      "string",
      null,
      { kind: "warn", summary: "trailer" },
    ]);
    expect(parseRecentCards(json)).toEqual([
      { kind: "user", summary: "ok" },
      { kind: "warn", summary: "trailer" },
    ]);
  });
});

describe("parseSlashMatches / parseSessions edge cases", () => {
  it("parseSlashMatches drops malformed entries", () => {
    const json = JSON.stringify([
      { cmd: "/ok", summary: "ok" },
      { cmd: "/no-summary" },
      { summary: "no-cmd" },
      null,
      "x",
      { cmd: "/with-args", summary: "x", argsHint: "<a>" },
    ]);
    expect(parseSlashMatches(json)).toEqual([
      { cmd: "/ok", summary: "ok" },
      { cmd: "/with-args", summary: "x", argsHint: "<a>" },
    ]);
  });

  it("parseSessions accepts entries missing the optional meta", () => {
    const json = JSON.stringify([{ title: "a" }, { title: "b", meta: "main" }]);
    expect(parseSessions(json)).toEqual([{ title: "a" }, { title: "b", meta: "main" }]);
  });

  it("parseSessions returns [] on garbage input", () => {
    expect(parseSessions(undefined)).toEqual([]);
    expect(parseSessions("oops")).toEqual([]);
    expect(parseSessions('{"not":"array"}')).toEqual([]);
  });
});
