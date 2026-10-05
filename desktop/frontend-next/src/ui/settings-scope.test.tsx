// @vitest-environment jsdom
import { afterEach, describe, expect, it } from "vitest";
import { cleanup, render } from "@testing-library/react";
import { renderToStaticMarkup } from "react-dom/server";
import "./testkit";
import { ApplyNote, Group, SCOPE_SAID } from "./Group";
import { SETTINGS } from "./prefsnav";

afterEach(cleanup);

describe("what a settings block says about itself", () => {
  // Two questions, and until now the screen could only answer one of them.
  it("answers who owns it and when it lands, in that order", () => {
    const html = renderToStaticMarkup(<Group id="context" title="上下文维护" />);
    expect(html).toContain("本机 · 重建运行时");
  });

  it("says who owns a block that changes nothing, and claims no timing", () => {
    const html = renderToStaticMarkup(<Group id="usage" title="用量与成本" />);
    expect(html).toContain("本机");
    expect(html).not.toMatch(/立即生效|重建运行时|重启/);
  });

  // A block that draws its own frame asks the same question, so the note is
  // reachable on its own — the appearance page builds its sections by hand.
  it("is available to a block that builds its own frame", () => {
    expect(renderToStaticMarkup(<ApplyNote id="language" />)).toContain("本机");
  });

  // Ownership is a fact. Thirty facts down a page must not read as thirty
  // alarms, so the scope rides the same weak line the timing already used.
  it("puts ownership at the weight of a fact, not a warning", () => {
    const { container } = render(<Group id="approval" title="工具批准" />);
    const note = container.querySelector(".apply");
    expect(note?.getAttribute("data-scope")).toBe("machine");
    expect(note?.className).not.toMatch(/warn|err|danger|accent/);
  });

  it("carries the scope as data, not only as a sentence", () => {
    const { container } = render(<Group id="preset" title="执行设定" />);
    expect(container.querySelector(".apply")?.getAttribute("data-scope")).toBe("session");
  });
});

describe("scope and timing stay two answers", () => {
  // Kept orthogonal on purpose: search, filtering, a command palette and an
  // export all want these separately, and a pre-joined sentence gives none of
  // them anything to read.
  it("keeps them as independent fields rather than one phrase", () => {
    for (const e of SETTINGS) {
      expect(typeof e.scope).toBe("string");
      expect(typeof e.apply).toBe("string");
      expect(e.scope).not.toContain("生效");
      expect(e.apply).not.toMatch(/session|machine|workspace|account/);
    }
  });

  // Every combination the audit produced is renderable; none of them is a pair
  // the label table has no word for.
  it("has a word for every scope the catalogue actually uses", () => {
    for (const e of SETTINGS) expect(SCOPE_SAID[e.scope], `${e.anchor} has no label`).toBeTruthy();
  });
});

describe("the search result, which is where a setting has no page around it", () => {
  // Settings takes fourteen props and loads on mount; what this gate is about
  // is not that screen's behaviour but where its two renderers get an answer
  // from, so it reads the source. The render half is covered above, on the
  // block, and both call the same table.
  const SRC = import.meta.glob(["./Settings.tsx", "./Group.tsx"], { query: "?raw", import: "default", eager: true }) as Record<string, string>;
  const src = (name: string) => Object.entries(SRC).find(([k]) => k.endsWith(name))![1].replace(/\s+/g, " ");

  // Sabotage C. The block keeps its scope while the result that leads to it
  // loses one — and the result is where a reader meets a setting with no page
  // around it to imply an owner. Searching 代理 must not leave them wondering
  // whether it is this project's proxy or this machine's.
  it("names the owner beside the setting", () => {
    expect(src("Settings.tsx"), "the settings search result stopped rendering ownership")
      .toMatch(/SCOPE_SAID\[e\.scope\]/);
  });

  it("takes it from the catalogue rather than a second table of its own", () => {
    expect(src("Settings.tsx")).toMatch(/import \{ Group, SCOPE_SAID \} from "\.\/Group"/);
    // One table, defined once. A second copy is how the block and the result
    // come to disagree about what a setting owns.
    expect(src("Group.tsx").match(/SCOPE_SAID[^=]*=/g)?.length ?? 0).toBe(1);
  });

  // The words live in one place. Checked on the taxonomy's own ids rather than
  // on the Chinese wording: 「当前会话」 is also a chip on the capability sheet
  // and 「当前模型」 sits in two unrelated hints, and a gate that matched prose
  // would call all three of them offenders. What must not exist twice is a
  // mapping keyed by the scope ids.
  it("has exactly one table turning a scope into words", () => {
    const all = import.meta.glob(["./**/*.{ts,tsx}", "!./**/*.test.*"], { query: "?raw", import: "default", eager: true }) as Record<string, string>;
    const ids = ["session", "model", "workspace", "machine", "account", "chosen"];
    const mappers = Object.entries(all)
      .filter(([, text]) => {
        const flat = text.replace(/\s+/g, " ");
        return ids.every((id) => flat.includes(`${id}:`)) && /Record<[^>]*Scope[^>]*>|SCOPE_SAID/.test(flat);
      })
      .map(([path]) => path);
    expect(mappers, "a second scope-to-words table is how the block and the search result come to disagree").toEqual(["./Group.tsx"]);
  });
});
