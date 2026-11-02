import { describe, expect, it } from "vitest";
import { inScope, listedAsLive, sessionRuntime } from "./sessionScope";

const live = { path: "/a.jsonl", name: "a", archived: false };
const archived = { path: "/b.jsonl", name: "b", archived: true };

describe("which list a conversation is in", () => {
  it("hides an archived conversation from the everyday list", () => {
    expect(inScope(archived, "all", false, false, false)).toBe(false);
  });
  it("finds it when searching, and only in the full list", () => {
    expect(inScope(archived, "all", true, false, false)).toBe(true);
    expect(inScope(archived, "pinned", true, false, true)).toBe(false);
    expect(inScope(archived, "live", true, true, false)).toBe(false);
  });
  it("lists it under Archived and nothing else there", () => {
    expect(inScope(archived, "archived", false, false, false)).toBe(true);
    expect(inScope(live, "archived", false, false, false)).toBe(false);
  });
});

describe("a finished session stays listed as running for the grace the reader chose", () => {
  const MIN = 60_000;

  it("lists a running session whatever the grace", () => {
    expect(listedAsLive(true, undefined, 0, 0)).toBe(true);
    expect(listedAsLive(true, undefined, 0, 30)).toBe(true);
  });

  it("drops a finished session at once when no grace was chosen", () => {
    expect(listedAsLive(false, 1_000, 1_001, 0)).toBe(false);
  });

  it("keeps a finished session until its grace has run out, and not a moment longer", () => {
    expect(listedAsLive(false, 0, 29 * MIN, 30)).toBe(true);
    expect(listedAsLive(false, 0, 30 * MIN, 30)).toBe(false);
  });

  it("never lists a session that was not seen finishing", () => {
    expect(listedAsLive(false, undefined, 5 * MIN, 30)).toBe(false);
  });
});

it("resolves open session identity by host and path without trusting a stale runtime ID", () => {
  const runtimes = [
    { id: "local", base: "", root: "/w", name: "local", sessionPath: "/same.jsonl" },
    { id: "remote", base: "", root: "/w", name: "remote", host: "gpu", sessionPath: "/same.jsonl" },
    { id: "changed", base: "", root: "/w", name: "changed", sessionPath: "/other.jsonl" },
  ];
  const session = { path: "/same.jsonl", name: "same", runtimeId: "changed" };
  expect(sessionRuntime(session, runtimes)?.id).toBe("local");
  expect(sessionRuntime(session, runtimes, "gpu")?.id).toBe("remote");
  expect(sessionRuntime({ ...session, path: "/closed.jsonl" }, runtimes)).toBeUndefined();
});
