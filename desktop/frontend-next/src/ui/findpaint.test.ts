// @vitest-environment jsdom
import { beforeEach, describe, expect, it } from "vitest";
import { finderFor, search } from "../state/find";
import type { Item } from "../state/session";
import { rangesIn } from "./findpaint";

const say = (id: string, text: string): Item => ({ t: "say", id, text, done: true });

function row(id: string, text: string): HTMLElement {
  const el = document.createElement("div");
  el.setAttribute("data-item", id);
  el.textContent = text;
  document.body.append(el);
  return el;
}

/** What the painter would draw for one row. */
function painted(text: string, query: string, mode = {}) {
  const finder = finderFor(query, mode);
  return finder ? rangesIn(row("i1", text), finder) : [];
}

beforeEach(() => {
  document.body.innerHTML = "";
});

describe("the painter draws what the counter counted", () => {
  it("paints one range per regex hit", () => {
    const text = "alpha beta gamma";
    const hits = search([say("i1", text)], "\\w+a", false, { regex: true });
    expect(painted(text, "\\w+a", { regex: true })).toHaveLength(hits.length);
  });

  it("paints one range per case-sensitive hit", () => {
    const text = "Retry retry RETRY";
    const hits = search([say("i1", text)], "retry", false, { caseSensitive: true });
    expect(painted(text, "retry", { caseSensitive: true })).toHaveLength(hits.length);
  });

  it("counts a greedy match once, not once per position", () => {
    expect(search([say("i1", "aaaa")], "a+", false, { regex: true })).toHaveLength(1);
    expect(painted("aaaa", "a+", { regex: true })).toHaveLength(1);
  });

  it("counts and paints nothing for an empty match", () => {
    // `^` and `a*` match the empty string; a hit with no range is one the box
    // can neither highlight nor scroll to.
    expect(search([say("i1", "alphabet")], "^", false, { regex: true })).toHaveLength(0);
    expect(painted("alphabet", "^", { regex: true })).toHaveLength(0);
    expect(search([say("i1", "bbb")], "a*", false, { regex: true })).toHaveLength(0);
    expect(painted("bbb", "a*", { regex: true })).toHaveLength(0);
  });

  it("still paints a plain search case-insensitively", () => {
    expect(painted("One one ONE", "one")).toHaveLength(3);
  });
});
