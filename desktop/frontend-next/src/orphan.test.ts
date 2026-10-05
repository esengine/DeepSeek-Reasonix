import { describe, expect, it } from "vitest";

// A component nobody renders still typechecks, still lints, and still passes
// every test that reads it. Plan.tsx and panels/Overview.tsx sat like that for
// weeks — the plan block the design calls for was written, styled, and simply
// never mounted. Nothing but this notices.
//
// Vite reads the sources; the frontend carries no Node types, so a check that
// needed them would only typecheck by accident.
const SOURCES = import.meta.glob(["./**/*.{ts,tsx}", "../perf/**/*.{ts,tsx}", "!./**/*.test.*"], {
  query: "?raw",
  import: "default",
  eager: true,
}) as Record<string, string>;

// Entry points are reached by the bundler, not by an import. Declared, never
// inferred — nothing in a filename says whether something mounts it.
const ENTRIES = new Set(["src/main.tsx", "perf/harness.tsx"]);

/** One spelling for every file, rooted at the project rather than at whichever
 *  directory the glob happened to resolve it from. */
const root = (key: string): string => (key.startsWith("../") ? key.slice(3) : "src/" + key.slice(2));

function resolve(from: string, spec: string): string {
  const stack = from.split("/").slice(0, -1);
  for (const part of spec.split("/")) {
    if (part === "." || part === "") continue;
    if (part === "..") stack.pop();
    else stack.push(part);
  }
  return stack.join("/");
}

describe("the source tree", () => {
  it("mounts every component it carries", () => {
    const reached = new Set<string>();
    for (const [key, body] of Object.entries(SOURCES)) {
      const from = root(key);
      for (const m of body.matchAll(/(?:from|import)\s*\(?\s*["']([^"']+)["']/g)) {
        if (m[1].startsWith(".")) reached.add(resolve(from, m[1]));
      }
    }
    const orphans = Object.keys(SOURCES)
      .map(root)
      .filter((f) => f.endsWith(".tsx") && !ENTRIES.has(f))
      // A path is written without its extension, and a directory import lands
      // on its index — a file counts as reached under either spelling.
      .filter((f) => {
        const bare = f.replace(/\.tsx$/, "");
        return !reached.has(bare) && !reached.has(bare.replace(/\/index$/, ""));
      });
    expect(orphans, "components nothing imports — mount them or delete them").toEqual([]);
  });

  it("reads the sources it is meant to be guarding", () => {
    expect(Object.keys(SOURCES).length).toBeGreaterThan(80);
  });
});
