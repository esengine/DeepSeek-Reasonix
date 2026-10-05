import { describe, expect, it } from "vitest";

// A judgement may not be made on a sentence.
//
// Two places used to compare a rendered Chinese string to decide something: a
// skill row read "调不到" back to pick its mark, and the session reducer
// compared "等待工作区" to know which state it was leaving. Both worked, and
// both were one rewrite of that sentence away from never matching again —
// silently, because a comparison that stops matching renders something
// plausible rather than failing. A 622-string copy pass is exactly the event
// that breaks them, so this is a ceiling of zero rather than a ratchet.
//
// Naming the value is enough to pass: a constant is one definition, and a
// rewrite moves the comparison with the sentence instead of past it. What is
// refused is the literal at the comparison site.
const SOURCES = import.meta.glob(["./**/*.{ts,tsx}", "!./**/*.test.*", "!./i18n/en*.ts"], {
  query: "?raw",
  import: "default",
  eager: true,
}) as Record<string, string>;

// The operators that turn a sentence into a branch. Membership in a catalogue
// (`DICT["…"]`) is not one of them: that is a lookup keyed by the text, which
// is what a catalogue is for.
const COMPARED = /(?:===|!==|==|!=)\s*"([^"\\]*[\u4e00-\u9fff][^"\\]*)"|"([^"\\]*[\u4e00-\u9fff][^"\\]*)"\s*(?:===|!==|==|!=)|\.(?:includes|startsWith|endsWith|indexOf|search)\(\s*"([^"\\]*[\u4e00-\u9fff][^"\\]*)"/g;

function judgements(src: string): string[] {
  const out: string[] = [];
  for (const line of src.split("\n")) {
    // Comments explain the rule; they do not run it.
    const code = line.replace(/\/\/.*$/, "").replace(/\/\*.*?\*\//g, "");
    for (const m of code.matchAll(COMPARED)) out.push((m[1] ?? m[2] ?? m[3]).slice(0, 30));
  }
  return out;
}

describe("what the interface is allowed to decide from", () => {
  it("never branches on a sentence it renders", () => {
    const found: string[] = [];
    for (const [path, src] of Object.entries(SOURCES)) {
      for (const hit of judgements(src)) found.push(`${path}  “${hit}”`);
    }
    expect(found, "compare a named constant, not the rendered text").toEqual([]);
  });

  // The guard's own regression: it has to catch each shape and let the
  // catalogue lookups through, or a clean run says nothing.
  it("catches every shape it claims to and no more", () => {
    for (const bad of [
      'if (note === "调不到") mark();',
      'if ("等待工作区" !== s.doing) go();',
      'if (label.includes("已完成")) done();',
      'if (title.startsWith("计划")) plan();',
    ]) {
      expect(judgements(bad), bad).not.toHaveLength(0);
    }
    for (const fine of [
      'const SCOPE = { project: "项目" };',
      'return t("调不到");',
      'if (note === UNREACHABLE) mark();',
      '// note === "调不到" was how this used to read',
      'out.push({ label: "常驻进程" });',
    ]) {
      expect(judgements(fine), fine).toHaveLength(0);
    }
  });
});
