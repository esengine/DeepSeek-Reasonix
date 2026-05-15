import { existsSync, mkdtempSync, readFileSync, rmSync, utimesSync, writeFileSync } from "node:fs";
import { homedir, tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { DeepSeekClient } from "../src/client.js";
import { CacheFirstLoop } from "../src/loop.js";
import { ImmutablePrefix } from "../src/memory/runtime.js";
import {
  appendSessionMessage,
  archiveSession,
  deleteSession,
  findSessionsByPrefix,
  freshSessionName,
  listSessions,
  listSessionsForWorkspace,
  loadSessionMessages,
  normalizeWorkspace,
  patchSessionMeta,
  pruneStaleSessions,
  renameSession,
  resolveSession,
  sanitizeName,
  sessionPath,
  sessionsDir,
  timestampSuffix,
} from "../src/memory/session.js";

describe("sanitizeName", () => {
  it("keeps alphanumerics, CJK, dashes, underscores", () => {
    expect(sanitizeName("hello-world_1")).toBe("hello-world_1");
    expect(sanitizeName("我的对话")).toBe("我的对话");
  });
  it("replaces other characters with underscore", () => {
    expect(sanitizeName("my/path:bad?")).toBe("my_path_bad_");
  });
  it("caps at 64 chars and defaults to 'default' when empty", () => {
    expect(sanitizeName("")).toBe("default");
    expect(sanitizeName("/:@!").length).toBeLessThanOrEqual(4);
    expect(sanitizeName("a".repeat(200))).toHaveLength(64);
  });
});

describe("session persistence", () => {
  let tmp: string;
  const realHome = homedir();

  beforeEach(() => {
    tmp = mkdtempSync(join(tmpdir(), "reasonix-session-"));
    vi.stubEnv("USERPROFILE", tmp); // Windows
    vi.stubEnv("HOME", tmp); // Unix
    // os.homedir() is cached per-process on some platforms — override via spy.
    vi.spyOn(require("node:os"), "homedir").mockReturnValue(tmp);
  });

  afterEach(() => {
    vi.restoreAllMocks();
    vi.unstubAllEnvs();
    if (existsSync(tmp)) rmSync(tmp, { recursive: true, force: true });
  });

  it("sessionPath lives under <home>/.reasonix/sessions", () => {
    const p = sessionPath("demo");
    expect(p).toContain(".reasonix");
    expect(p).toContain("sessions");
    expect(p.endsWith("demo.jsonl")).toBe(true);
    expect(p.startsWith(tmp)).toBe(true);
  });

  it("loadSessionMessages returns [] when the file doesn't exist", () => {
    expect(loadSessionMessages("ghost")).toEqual([]);
  });

  it("appendSessionMessage + loadSessionMessages round-trip", () => {
    appendSessionMessage("foo", { role: "user", content: "hi" });
    appendSessionMessage("foo", { role: "assistant", content: "hello" });
    const msgs = loadSessionMessages("foo");
    expect(msgs.length).toBe(2);
    expect(msgs[0]).toEqual({ role: "user", content: "hi" });
    expect(msgs[1]).toEqual({ role: "assistant", content: "hello" });
  });

  it("tolerates malformed lines (skips them)", () => {
    appendSessionMessage("mix", { role: "user", content: "a" });
    // inject a garbage line directly
    const p = sessionPath("mix");
    writeFileSync(p, `${readFileSync(p, "utf8")}not json\n`);
    appendSessionMessage("mix", { role: "user", content: "b" });
    const msgs = loadSessionMessages("mix");
    expect(msgs.length).toBe(2);
  });

  it("listSessions returns metadata sorted by mtime desc", () => {
    appendSessionMessage("alpha", { role: "user", content: "x" });
    appendSessionMessage("beta", { role: "user", content: "y" });
    appendSessionMessage("beta", { role: "user", content: "z" });
    const items = listSessions();
    expect(items.length).toBe(2);
    const names = items.map((s) => s.name);
    expect(names).toContain("alpha");
    expect(names).toContain("beta");
    const beta = items.find((s) => s.name === "beta")!;
    expect(beta.messageCount).toBe(2);
    expect(beta.size).toBeGreaterThan(0);
  });

  it("listSessions excludes .events.jsonl sidecars", () => {
    appendSessionMessage("real", { role: "user", content: "x" });
    writeFileSync(sessionPath("real").replace(/\.jsonl$/, ".events.jsonl"), '{"id":1}\n');
    const names = listSessions().map((s) => s.name);
    expect(names).toEqual(["real"]);
  });

  it("listSessionsForWorkspace matches meta.workspace and hides untagged sessions", () => {
    appendSessionMessage("here", { role: "user", content: "x" });
    appendSessionMessage("there", { role: "user", content: "x" });
    appendSessionMessage("untagged", { role: "user", content: "x" });
    patchSessionMeta("here", { workspace: "/proj/a" });
    patchSessionMeta("there", { workspace: "/proj/b" });
    const names = listSessionsForWorkspace("/proj/a").map((s) => s.name);
    expect(names).toEqual(["here"]);
  });

  it("listSessionsForWorkspace tolerates trailing-slash drift", () => {
    appendSessionMessage("a", { role: "user", content: "x" });
    patchSessionMeta("a", { workspace: "/proj/a/" });
    const names = listSessionsForWorkspace("/proj/a").map((s) => s.name);
    expect(names).toEqual(["a"]);
  });

  it("renameSession also moves the .events.jsonl sidecar", () => {
    appendSessionMessage("orig", { role: "user", content: "x" });
    const oldEvents = sessionPath("orig").replace(/\.jsonl$/, ".events.jsonl");
    writeFileSync(oldEvents, '{"id":1}\n');
    expect(renameSession("orig", "renamed")).toBe(true);
    expect(existsSync(oldEvents)).toBe(false);
    expect(existsSync(sessionPath("renamed").replace(/\.jsonl$/, ".events.jsonl"))).toBe(true);
  });

  it("deleteSession removes the .events.jsonl sidecar too", () => {
    appendSessionMessage("trash", { role: "user", content: "x" });
    const events = sessionPath("trash").replace(/\.jsonl$/, ".events.jsonl");
    writeFileSync(events, '{"id":1}\n');
    deleteSession("trash");
    expect(existsSync(events)).toBe(false);
  });

  it("deleteSession removes the file", () => {
    appendSessionMessage("gone", { role: "user", content: "x" });
    expect(existsSync(sessionPath("gone"))).toBe(true);
    expect(deleteSession("gone")).toBe(true);
    expect(existsSync(sessionPath("gone"))).toBe(false);
    expect(deleteSession("gone")).toBe(false);
  });

  it("deleteSession removes the plan-state sidecar too", () => {
    appendSessionMessage("plan-sidecar", { role: "user", content: "hi" });
    const planPath = sessionPath("plan-sidecar").replace(/\.jsonl$/, ".plan.json");
    writeFileSync(
      planPath,
      JSON.stringify({
        version: 1,
        steps: [{ id: "s1", title: "t", action: "a" }],
        completedStepIds: [],
        updatedAt: new Date().toISOString(),
      }),
    );
    expect(existsSync(planPath)).toBe(true);
    deleteSession("plan-sidecar");
    expect(existsSync(sessionPath("plan-sidecar"))).toBe(false);
    expect(existsSync(planPath)).toBe(false);
  });

  it("pruneStaleSessions deletes sessions older than the cutoff and leaves fresh ones", () => {
    // Three sessions: two backdated past the 90-day default, one
    // fresh. Backdate via utimesSync since createTime/mtime is what
    // listSessions reads.
    appendSessionMessage("ancient1", { role: "user", content: "x" });
    appendSessionMessage("ancient2", { role: "user", content: "x" });
    appendSessionMessage("recent", { role: "user", content: "x" });
    const oldDate = new Date(Date.now() - 200 * 24 * 60 * 60 * 1000);
    utimesSync(sessionPath("ancient1"), oldDate, oldDate);
    utimesSync(sessionPath("ancient2"), oldDate, oldDate);

    const removed = pruneStaleSessions(90);
    expect(removed.sort()).toEqual(["ancient1", "ancient2"]);
    expect(existsSync(sessionPath("ancient1"))).toBe(false);
    expect(existsSync(sessionPath("ancient2"))).toBe(false);
    expect(existsSync(sessionPath("recent"))).toBe(true);
  });

  it("pruneStaleSessions with a tighter cutoff catches sessions the default would skip", () => {
    appendSessionMessage("yesterday", { role: "user", content: "x" });
    const yest = new Date(Date.now() - 36 * 60 * 60 * 1000); // 1.5 days
    utimesSync(sessionPath("yesterday"), yest, yest);

    expect(pruneStaleSessions(90)).toEqual([]);
    expect(existsSync(sessionPath("yesterday"))).toBe(true);
    expect(pruneStaleSessions(1)).toEqual(["yesterday"]);
    expect(existsSync(sessionPath("yesterday"))).toBe(false);
  });

  describe("archiveSession", () => {
    it("returns null when the session file does not exist", () => {
      expect(archiveSession("ghost")).toBeNull();
    });

    it("returns null when the session file is empty", () => {
      appendSessionMessage("empty", { role: "user", content: "x" });
      writeFileSync(sessionPath("empty"), "");
      expect(archiveSession("empty")).toBeNull();
      expect(existsSync(sessionPath("empty"))).toBe(true);
    });

    it("renames jsonl + sidecars to a timestamped archive name", () => {
      appendSessionMessage("live", { role: "user", content: "hi" });
      const events = sessionPath("live").replace(/\.jsonl$/, ".events.jsonl");
      const meta = sessionPath("live").replace(/\.jsonl$/, ".meta.json");
      writeFileSync(events, '{"id":1}\n');
      writeFileSync(meta, "{}");

      const archived = archiveSession("live");
      expect(archived).toMatch(/^live__archive_\d{12}/);
      expect(existsSync(sessionPath("live"))).toBe(false);
      expect(existsSync(sessionPath(archived!))).toBe(true);
      expect(existsSync(events)).toBe(false);
      expect(existsSync(meta)).toBe(false);
      expect(existsSync(sessionPath(archived!).replace(/\.jsonl$/, ".events.jsonl"))).toBe(true);
      expect(existsSync(sessionPath(archived!).replace(/\.jsonl$/, ".meta.json"))).toBe(true);
      expect(loadSessionMessages(archived!)).toEqual([{ role: "user", content: "hi" }]);
    });

    it("disambiguates when called twice in the same minute", () => {
      appendSessionMessage("rapid", { role: "user", content: "first" });
      const a = archiveSession("rapid");
      appendSessionMessage("rapid", { role: "user", content: "second" });
      const b = archiveSession("rapid");
      expect(a).not.toBeNull();
      expect(b).not.toBeNull();
      expect(a).not.toBe(b);
      expect(loadSessionMessages(a!)).toEqual([{ role: "user", content: "first" }]);
      expect(loadSessionMessages(b!)).toEqual([{ role: "user", content: "second" }]);
    });

    it("archive name is excluded from the resume-by-prefix lookup", () => {
      appendSessionMessage("proj", { role: "user", content: "x" });
      const archived = archiveSession("proj");
      expect(archived).not.toBeNull();
      expect(findSessionsByPrefix("proj-")).toEqual([]);
    });

    it("archive shows up in listSessions", () => {
      appendSessionMessage("show", { role: "user", content: "x" });
      const archived = archiveSession("show");
      const names = listSessions().map((s) => s.name);
      expect(names).toContain(archived);
    });
  });

  describe("clearLog archive integration", () => {
    it("CacheFirstLoop.clearLog archives the live transcript and starts an empty file", () => {
      const client = new DeepSeekClient({
        apiKey: "sk-test",
        fetch: (async () => new Response()) as any,
      });
      const loop = new CacheFirstLoop({
        client,
        prefix: new ImmutablePrefix({ system: "s" }),
        stream: false,
        session: "clear-archive",
      });
      loop.appendAndPersist({ role: "user", content: "first turn" });
      loop.appendAndPersist({ role: "assistant", content: "reply" });

      const { dropped, archived } = loop.clearLog();
      expect(dropped).toBe(2);
      expect(archived).toMatch(/^clear-archive__archive_\d{12}/);
      expect(loadSessionMessages(archived!)).toHaveLength(2);
      expect(loadSessionMessages("clear-archive")).toEqual([]);
      expect(loop.log.length).toBe(0);
    });

    it("clearLog returns archived: null when the session has nothing on disk", () => {
      const client = new DeepSeekClient({
        apiKey: "sk-test",
        fetch: (async () => new Response()) as any,
      });
      const loop = new CacheFirstLoop({
        client,
        prefix: new ImmutablePrefix({ system: "s" }),
        stream: false,
        session: "clear-empty",
      });
      const { archived } = loop.clearLog();
      expect(archived).toBeNull();
    });
  });

  it("sessionsDir exists after first append", () => {
    appendSessionMessage("s", { role: "user", content: "x" });
    expect(existsSync(sessionsDir())).toBe(true);
    expect(existsSync(dirname(sessionPath("s")))).toBe(true);
  });

  it("loop.appendAndPersist writes bang-style messages to the session file", () => {
    // Regression: before 0.5.14 the bang handler called loop.log.append which
    // only touched memory, so `!cmd` output was lost on session resume.
    const client = new DeepSeekClient({
      apiKey: "sk-test",
      fetch: (async () => new Response()) as any,
    });
    const loop = new CacheFirstLoop({
      client,
      prefix: new ImmutablePrefix({ system: "s" }),
      stream: false,
      session: "bang-persist",
    });
    loop.appendAndPersist({ role: "user", content: "[!ls]\n$ ls\n[exit 0]\nfile1 file2" });
    const reloaded = loadSessionMessages("bang-persist");
    expect(reloaded).toEqual([{ role: "user", content: "[!ls]\n$ ls\n[exit 0]\nfile1 file2" }]);
  });

  describe("timestampSuffix", () => {
    it("returns a 12-character string of digits", () => {
      const ts = timestampSuffix();
      expect(ts).toMatch(/^\d{12}$/);
    });

    it("starts with the current year", () => {
      const year = String(new Date().getFullYear());
      expect(timestampSuffix().startsWith(year)).toBe(true);
    });

    it("is sortable — later calls produce lexicographically larger strings", () => {
      const a = timestampSuffix();
      const b = timestampSuffix();
      // In the unlikely event both fall on the same minute, they're equal
      expect(b.localeCompare(a)).toBeGreaterThanOrEqual(0);
    });
  });

  describe("freshSessionName", () => {
    it("defaults to a default-prefixed name when no current session", () => {
      expect(freshSessionName(undefined)).toMatch(/^default-\d{14}$/);
    });

    it("preserves a non-timestamped base", () => {
      expect(freshSessionName("foo")).toMatch(/^foo-\d{14}$/);
    });

    it("strips an existing 12-digit timestamp suffix before re-stamping", () => {
      expect(freshSessionName("foo-202605120800")).toMatch(/^foo-\d{14}$/);
    });

    it("strips an existing 14-digit timestamp suffix before re-stamping", () => {
      expect(freshSessionName("foo-20260512080000")).toMatch(/^foo-\d{14}$/);
    });

    it("keeps dashed bases intact (only the trailing timestamp is stripped)", () => {
      expect(freshSessionName("my-app-bar")).toMatch(/^my-app-bar-\d{14}$/);
    });
  });

  describe("resolveSession", () => {
    it("returns the base name when no prior sessions and no flags", () => {
      const { resolved, preview } = resolveSession("fresh");
      expect(resolved).toBe("fresh");
      expect(preview).toBeUndefined();
    });

    it("generates a timestamped name on forceNew", () => {
      const { resolved, preview } = resolveSession("demo", true);
      expect(resolved).toMatch(/^demo-\d{12}$/);
      expect(preview).toBeUndefined();
    });

    it("returns undefined when sessionName is undefined", () => {
      const { resolved, preview } = resolveSession(undefined);
      expect(resolved).toBeUndefined();
      expect(preview).toBeUndefined();
    });

    it("picks the base name when no prefixed sessions exist and it has messages", () => {
      appendSessionMessage("project", { role: "user", content: "hello" });
      const { resolved, preview } = resolveSession("project");
      expect(resolved).toBe("project");
      expect(preview).toBeDefined();
      expect(preview!.messageCount).toBe(1);
    });

    it("ignores timestamped sessions that have only .events.jsonl (no messages file)", () => {
      appendSessionMessage("myproject", { role: "user", content: "real messages" });
      const eventsPath = sessionPath("myproject-20260430T200000").replace(
        /\.jsonl$/,
        ".events.jsonl",
      );
      writeFileSync(eventsPath, "{}");

      const { resolved, preview } = resolveSession("myproject");
      expect(resolved).toBe("myproject");
      expect(preview).toBeDefined();
      expect(preview!.messageCount).toBe(1);
    });

    it("picks the latest prefixed session over the base name", () => {
      appendSessionMessage("project", { role: "user", content: "old" });
      appendSessionMessage("project-20260430T091500", { role: "user", content: "newer" });
      // Create a later timestamp so it sorts first
      const evenLater = new Date(Date.now() + 5000);
      appendSessionMessage("project-20260430T154500", { role: "user", content: "newest" });
      utimesSync(sessionPath("project-20260430T154500"), evenLater, evenLater);

      const { resolved, preview } = resolveSession("project");
      // Bare "project" is excluded — prefix lookup uses "project-" (with dash).
      expect(resolved).toBe("project-20260430T154500");
      expect(preview).toBeDefined();
    });

    it("forceResume resolves to the latest prefixed session", () => {
      appendSessionMessage("app", { role: "user", content: "a" });
      appendSessionMessage("app-20260430T091500", { role: "user", content: "b" });
      const { resolved, preview } = resolveSession("app", false, true);
      expect(resolved).toBe("app-20260430T091500");
      expect(preview).toBeUndefined();
    });

    it("forceResume falls back to base name when no prefixed sessions exist", () => {
      const { resolved, preview } = resolveSession("standalone", false, true);
      expect(resolved).toBe("standalone");
      expect(preview).toBeUndefined();
    });
  });

  describe("findSessionsByPrefix", () => {
    it("returns [] when the sessions directory does not exist", () => {
      const dir = sessionsDir();
      if (existsSync(dir)) rmSync(dir, { recursive: true, force: true });
      expect(findSessionsByPrefix("anything")).toEqual([]);
    });

    it("returns session names matching the prefix, sorted alpha-reverse", () => {
      // Filename sort — zero-padded YYYYMMDDHHmm sorts newest-first after reverse.
      // Non-digit suffixes (letters > digits in ASCII) sort above timestamps.
      appendSessionMessage("code-reasonix-old", { role: "user", content: "x" });
      appendSessionMessage("code-reasonix-20260430T143200", { role: "user", content: "y" });
      appendSessionMessage("code-reasonix-20260430T154500", { role: "user", content: "z" });

      const result = findSessionsByPrefix("code-reasonix-");
      expect(result).toEqual([
        "code-reasonix-old",
        "code-reasonix-20260430T154500",
        "code-reasonix-20260430T143200",
      ]);
    });

    it("does not return sessions that don't start with the prefix", () => {
      appendSessionMessage("foo-bar", { role: "user", content: "a" });
      appendSessionMessage("foo-baz", { role: "user", content: "b" });
      appendSessionMessage("other-thing", { role: "user", content: "c" });

      expect(findSessionsByPrefix("foo-")).toEqual(["foo-baz", "foo-bar"]);
    });

    it("only matches .jsonl files, not sidecar files", () => {
      appendSessionMessage("alpha-001", { role: "user", content: "x" });
      writeFileSync(sessionPath("alpha-001").replace(/\.jsonl$/, ".plan.json"), "{}");
      writeFileSync(sessionPath("alpha-001").replace(/\.jsonl$/, ".pending.json"), "{}");
      writeFileSync(sessionPath("alpha-001").replace(/\.jsonl$/, ".events.jsonl"), "{}");

      const result = findSessionsByPrefix("alpha-");
      expect(result).toEqual(["alpha-001"]);
    });

    it("prefix with trailing dash excludes the bare base session name", () => {
      appendSessionMessage("project", { role: "user", content: "a" });
      appendSessionMessage("project-20260430T143200", { role: "user", content: "b" });

      expect(findSessionsByPrefix("project-")).toEqual(["project-20260430T143200"]);
      // No-dash prefix matches both; reverse-sort puts the bare name first ('.' > '-' in ASCII).
      expect(findSessionsByPrefix("project")).toEqual(["project", "project-20260430T143200"]);
    });
  });

  describe("issue #333 — resume seeds cost carryover from session meta", () => {
    it("CacheFirstLoop on resume preloads totalCostUsd / turnCount into stats", () => {
      appendSessionMessage("c333", { role: "user", content: "hi" });
      appendSessionMessage("c333", { role: "assistant", content: "hello" });
      patchSessionMeta("c333", { totalCostUsd: 0.0123, turnCount: 5 });

      const client = new DeepSeekClient({ apiKey: "sk-test" });
      const loop = new CacheFirstLoop({
        client,
        prefix: new ImmutablePrefix({ system: "test" }),
        session: "c333",
      });

      expect(loop.stats.totalCost).toBe(0.0123);
      const summary = loop.stats.summary();
      expect(summary.totalCostUsd).toBe(0.0123);
      expect(summary.turns).toBe(5);
    });

    it("fresh session (no meta) leaves carryover at zero", () => {
      const client = new DeepSeekClient({ apiKey: "sk-test" });
      const loop = new CacheFirstLoop({
        client,
        prefix: new ImmutablePrefix({ system: "test" }),
        session: "fresh-c333",
      });
      expect(loop.stats.totalCost).toBe(0);
      expect(loop.stats.summary().turns).toBe(0);
    });
  });

  describe("issue #364 — resume seeds cache + lastPromptTokens from session meta", () => {
    it("CacheFirstLoop on resume preloads cache totals + last prompt tokens", () => {
      appendSessionMessage("c364", { role: "user", content: "hi" });
      appendSessionMessage("c364", { role: "assistant", content: "hello" });
      patchSessionMeta("c364", {
        cacheHitTokens: 366976,
        cacheMissTokens: 109,
        lastPromptTokens: 367085,
      });

      const client = new DeepSeekClient({ apiKey: "sk-test" });
      const loop = new CacheFirstLoop({
        client,
        prefix: new ImmutablePrefix({ system: "test" }),
        session: "c364",
      });

      const summary = loop.stats.summary();
      expect(summary.cacheHitRatio).toBeCloseTo(366976 / (366976 + 109), 4);
      expect(summary.lastPromptTokens).toBe(367085);
    });
  });
});

describe("normalizeWorkspace", () => {
  it("collapses trailing slashes and `.` segments on posix", () => {
    expect(normalizeWorkspace("/proj/a/", "linux")).toBe("/proj/a");
    expect(normalizeWorkspace("/proj/./a", "linux")).toBe("/proj/a");
  });

  it("lowercases drive letter and unifies separators on win32", () => {
    expect(normalizeWorkspace("C:\\Users\\Foo\\proj", "win32")).toBe("c:/Users/Foo/proj");
    expect(normalizeWorkspace("c:/users/foo/proj", "win32")).toBe("c:/users/foo/proj");
  });

  it("yields the same canonical form for win32 drive-case + separator variants", () => {
    const variants = [
      "C:\\Users\\foo\\proj",
      "c:\\Users\\foo\\proj",
      "C:/Users/foo/proj",
      "c:/Users/foo/proj/",
    ];
    const canonicals = variants.map((v) => normalizeWorkspace(v, "win32"));
    for (const c of canonicals) expect(c).toBe(canonicals[0]);
  });

  it("returns empty string for undefined or empty input", () => {
    expect(normalizeWorkspace(undefined)).toBe("");
    expect(normalizeWorkspace("")).toBe("");
  });
});
