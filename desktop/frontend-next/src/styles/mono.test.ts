import { parse } from "@babel/parser";
import { describe, expect, it } from "vitest";

// The monospaced face says "a machine produced this": a path, a command, an id,
// a column of figures that must not jump, a mark that has to hold its cell.
// Interface prose is none of those, and a window that sets its own sentences in
// it reads as a terminal rather than as something written.
//
// The judgement is structural, never a look at the words: a class is monospaced
// if a rule in app.css gives it var(--mono), and an element renders prose if its
// own children call t(). Nothing here reads what a string says.
const CSS = import.meta.glob("./app.css", { query: "?raw", import: "default", eager: true }) as Record<string, string>;
const SOURCES = import.meta.glob(["../**/*.tsx", "!../**/*.test.*"], {
  query: "?raw",
  import: "default",
  eager: true,
}) as Record<string, string>;

// Monospace earns these: they are marks and identifiers, not sentences. The
// welcome screen is the one deliberate exception — the wordmark and its eyebrow
// are set in the terminal's own face because that is the product's vernacular.
const MARKS: Record<string, string> = {
  x: "× — the close mark, sized to hold its cell beside ✓",
  "ptab-x": "× — same mark on a pane tab",
  ah: "› — the affordance arrow",
  prompt: "› — the composer's own prompt character",
  esc: "Esc — a keycap, which is lettering on a key rather than prose",
  "oobe-brand": "the wordmark, deliberately set in the terminal's face",
  "oobe-sub": "the wordmark's eyebrow, set with it",
  "oobe-skip": "the third line of that same screen",
};

// Classes that render prose and data through the same box. Splitting one means
// moving var(--mono) off the class and onto the value inside it, which is a
// change per call site rather than per rule — recorded so the count cannot grow
// while the work is outstanding. Never widen this to land a change.
const MIXED = new Set([
  "c", "cnt", "cost", "cur", "d", "delta", "dt", "fail", "isolab", "k",
  "lb", "meta", "mk", "n", "nm", "ntimes", "path", "pill", "pt", "r", "rmtcand", "rt",
  "sc", "sessmeta", "st", "sym", "sz", "tag", "u", "uspan", "v", "wsmeta",
]);

const TRANSLATORS = ["t", "tx", "plural"];

function monoClasses(css: string): Set<string> {
  const lines = css.split("\n");
  const out = new Set<string>();
  for (let i = 0; i < lines.length; i++) {
    if (!lines[i].includes("var(--mono)")) continue;
    let sel: string | null = null;
    if (lines[i].includes("{")) sel = lines[i].split("{")[0];
    else
      for (let j = i - 1; j >= 0 && j > i - 12; j--)
        if (lines[j].includes("{")) { sel = lines[j].split("{")[0]; break; }
    if (!sel) continue;
    for (const one of sel.split(",")) {
      const last = one.trim().split(/\s+|>/).filter(Boolean).pop() ?? "";
      // A rule on ::before styles the pseudo-element's own content, not the text
      // the element carries.
      if (last.includes("::")) continue;
      for (const m of last.matchAll(/\.([A-Za-z][\w-]*)/g)) out.add(m[1]);
    }
  }
  return out;
}

/** Whether this node calls a translator, not counting nested elements — those
 *  are boxes of their own and answer for their own face. */
function callsTranslator(node: any, names: Set<string>): boolean {
  let found = false;
  const dig = (x: any): void => {
    if (!x || typeof x.type !== "string" || x.type === "JSXElement" || found) return;
    if (x.type === "CallExpression" && x.callee.type === "Identifier" && names.has(x.callee.name)) {
      found = true;
      return;
    }
    for (const k of Object.keys(x)) {
      const v = x[k];
      if (Array.isArray(v)) v.forEach(dig);
      else if (v && typeof v === "object" && v.type) dig(v);
    }
  };
  dig(node);
  return found;
}

function translators(src: string): Set<string> {
  const names = new Set(TRANSLATORS);
  for (const m of src.matchAll(/import\s*\{([^}]*)\}\s*from\s*"[^"]*i18n[^"]*"/g))
    for (const part of m[1].split(",")) {
      const [orig, alias] = part.split(/\s+as\s+/).map((x) => x.trim());
      if (TRANSLATORS.includes(orig)) names.add(alias || orig);
    }
  return names;
}

type Landing = { cls: string; prose: boolean; data: boolean; where: string };

function landings(mono: Set<string>): Landing[] {
  const out: Landing[] = [];
  for (const [file, src] of Object.entries(SOURCES)) {
    const names = translators(src);
    const ast = parse(src, { sourceType: "module", plugins: ["typescript", "jsx"] });
    const visit = (n: any): void => {
      if (!n || typeof n.type !== "string") return;
      if (n.type === "JSXElement") {
        const attr = n.openingElement.attributes.find(
          (a: any) => a.type === "JSXAttribute" && a.name?.name === "className" && a.value?.type === "StringLiteral",
        );
        const hit = attr ? attr.value.value.split(/\s+/).filter((c: string) => mono.has(c)) : [];
        if (hit.length) {
          let prose = false, data = false;
          for (const ch of n.children) {
            if (ch.type === "JSXText" && ch.value.trim()) prose = true;
            else if (ch.type === "JSXExpressionContainer")
              callsTranslator(ch.expression, names) ? (prose = true) : (data = true);
          }
          if (prose || data)
            out.push({ cls: hit[0], prose, data, where: `${file.replace(/^\.\.\//, "")}:${n.loc.start.line}` });
        }
      }
      for (const k of Object.keys(n)) {
        const v = n[k];
        if (Array.isArray(v)) v.forEach(visit);
        else if (v && typeof v === "object" && v.type) visit(v);
      }
    };
    visit(ast);
  }
  return out;
}

describe("monospace is for machine facts", () => {
  const mono = monoClasses(Object.values(CSS)[0]);
  const all = landings(mono);
  const byClass = new Map<string, Landing[]>();
  for (const l of all) byClass.set(l.cls, [...(byClass.get(l.cls) ?? []), l]);

  it("does not set interface prose in the monospaced face", () => {
    const offenders: string[] = [];
    for (const [cls, list] of byClass) {
      if (!list.some((l) => l.prose)) continue;      // renders data only
      if (list.some((l) => l.data)) continue;        // mixed, held below
      if (cls in MARKS || MIXED.has(cls)) continue;
      offenders.push(`.${cls} — ${list.filter((l) => l.prose).map((l) => l.where).join(", ")}`);
    }
    expect(offenders, "prose set in var(--mono); give the class var(--ui), or add it to MARKS with its reason").toEqual([]);
  });

  it("does not grow the set of classes carrying both prose and data", () => {
    const grown = [...byClass]
      .filter(([cls, list]) => list.some((l) => l.prose) && list.some((l) => l.data) && !MIXED.has(cls) && !(cls in MARKS))
      .map(([cls, list]) => `.${cls} — ${list[0].where}`);
    expect(grown, "a new class renders prose and data through one monospaced box; move var(--mono) onto the value").toEqual([]);
  });

  it("keeps every recorded exception in use", () => {
    const live = new Set([...byClass].filter(([, l]) => l.some((x) => x.prose)).map(([c]) => c));
    const stale = [...Object.keys(MARKS), ...MIXED].filter((c) => !live.has(c));
    expect(stale, "listed as an exception but no longer renders prose in a monospaced class — drop it").toEqual([]);
  });
});
