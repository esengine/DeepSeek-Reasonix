// @vitest-environment jsdom
import { describe, expect, it, afterEach } from "vitest";
import { apply } from "./theme";
import type { ThemePack } from "../port/port";

const pack: ThemePack = {
  id: "probe",
  name: "Probe",
  tokens: {
    light: { bg: "#FFFFFF", bgSoft: "#F4F4F4", fg: "#1A1A1A", fgDim: "#5A5A5A", fgFaint: "#767676", accent: "#0066CC" },
    dark: { bg: "#101010", bgSoft: "#181818", fg: "#F0F0F0", fgDim: "#B0B0B0", fgFaint: "#8A8A8A", accent: "#66AAFF" },
  },
};

const illustrated: ThemePack = {
  ...pack,
  background: { image: true, focusX: 0.5, focusY: 0.5, safeArea: "left", homeOpacity: 0.96, taskOpacity: 0.18, overlayStrength: 0.72 },
};

const sheet = () => document.getElementById("pack-theme") as HTMLStyleElement | null;
const rule = () => sheet()?.sheet?.cssRules[0] as CSSStyleRule | undefined;
const read = (name: string) => rule()?.style.getPropertyValue(name).trim() ?? "";
const inline = () => document.documentElement.style.length;

afterEach(() => apply(null, "light"));

describe("a pack is delivered by a generated stylesheet", () => {
  it("leaves no custom property on the root's inline style", () => {
    apply(illustrated, "light", false, "strong");
    expect(inline()).toBe(0);
    expect(read("--page")).toBe("#FFFFFF");
    expect(read("--bg-image")).toContain("/themes/probe/background");
  });

  it("paints no ink inline when there is no pack, and owns nothing", () => {
    apply(null, "light", false, "strong");
    expect(inline()).toBe(0);
    expect(sheet()).toBeNull();
  });

  it("sits after every built-in stylesheet and stays before an author's later one", () => {
    const builtin = document.createElement("style");
    document.head.append(builtin);
    apply(pack, "light");
    expect(builtin.compareDocumentPosition(sheet()!) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    const author = document.createElement("style");
    document.head.append(author);
    apply(pack, "dark");
    expect(sheet()!.compareDocumentPosition(author) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    builtin.remove();
    author.remove();
  });

  it("loses a tie to a stylesheet that was already in the document", () => {
    const earlier = document.createElement("style");
    document.head.append(earlier);
    apply(pack, "light");
    expect(earlier.compareDocumentPosition(sheet()!) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    earlier.remove();
  });

  it("is matched at the specificity of the built-in contrast tiers, so source order decides", () => {
    apply(pack, "light");
    expect(rule()?.selectorText).toBe(":root:root[data-pack]");
  });

  it("is replaced as one unit and removed with the pack", () => {
    apply(pack, "light");
    expect(document.documentElement.dataset.pack).toBe("probe");
    apply({ ...pack, id: "other", tokens: { light: { bg: "#EEEEEE" }, dark: {} } }, "light");
    expect(document.querySelectorAll("#pack-theme")).toHaveLength(1);
    expect(read("--page")).toBe("#EEEEEE");
    expect(read("--accent")).toBe("");
    apply(null, "light");
    expect(sheet()).toBeNull();
    expect(document.documentElement.dataset.pack).toBeUndefined();
  });

  it("follows the scheme", () => {
    apply(pack, "light");
    expect(read("--page")).toBe("#FFFFFF");
    apply(pack, "dark");
    expect(read("--page")).toBe("#101010");
  });

  it("keeps a value that would end a rule inside its declaration", () => {
    apply({ ...pack, tokens: { light: { bg: "#fff} body{display:none", fg: "#111" }, dark: {} } }, "light");
    expect(sheet()?.sheet?.cssRules).toHaveLength(1);
    expect(sheet()?.textContent).toBe("");
    expect(read("--text")).toBe("#111");
  });
});

describe("a pack's inks answer to the reader's contrast", () => {
  // The setting is the reader's, the way reading size is. A pack that pinned
  // --text inline won every step of it, so someone who had asked for stronger
  // text got the author's answer instead for as long as the palette was on.
  it("moves the pack's own colours instead of being overruled by them", () => {
    apply(pack, "light", false, "strong");
    expect(read("--text")).toMatch(/^oklch\(from #1A1A1A calc\(l - 0\.101\) c h\)$/);
    expect(read("--muted")).toContain("#5A5A5A");
    expect(read("--faint")).toContain("#767676");
    expect(read("--ghost")).toBe(read("--faint"));
  });

  it("darkens in light and lightens in dark", () => {
    apply(pack, "light", false, "normal");
    expect(read("--text")).toContain("l - 0.03");
    apply(pack, "dark", false, "normal");
    expect(read("--text")).toContain("l + 0.03");
  });

  // The step a pack states is the one someone who never opened the setting
  // sees, so those two must land on the author's value untouched.
  it("leaves the pack's value alone on the default step", () => {
    apply(pack, "light", false, "");
    expect(read("--text")).toBe("#1A1A1A");
    apply(pack, "light", false, "soft");
    expect(read("--text")).toBe("#1A1A1A");
  });

  it("does not move a surface, only the inks", () => {
    apply(pack, "light", false, "strong");
    expect(read("--page")).toBe("#FFFFFF");
    expect(read("--accent")).toBe("#0066CC");
  });

  it("paints a pack's strong-text colour and steps it with the other inks", () => {
    const strong: ThemePack = { ...pack, tokens: { light: { ...pack.tokens.light, fgStrong: "#8A3B12" } } };
    apply(strong, "light", false, "");
    expect(read("--text-strong")).toBe("#8A3B12");
    apply(strong, "light", false, "strong");
    expect(read("--text-strong")).toMatch(/^oklch\(from #8A3B12 calc\(l - 0\.101\) c h\)$/);
  });

  it("leaves strong text on the body ink when the pack does not name it", () => {
    apply(pack, "light", false, "strong");
    expect(read("--text-strong")).toBe("");
  });

  it("uses a gentler illustration scrim in light mode", () => {
    apply(illustrated, "light");
    expect(Number(read("--bg-overlay"))).toBeCloseTo(0.72 * 0.58);
    apply(illustrated, "dark");
    expect(Number(read("--bg-overlay"))).toBeCloseTo(0.72);
  });
});

describe("decorative roles are themeable and the status hues are not", () => {
  const decorated: ThemePack = {
    ...pack,
    tokens: {
      light: { ...pack.tokens.light, link: "#111111", brand: "#222222", halo: "#333333", labelAgent: "#444444" },
      dark: { ...pack.tokens.dark, link: "#eeeeee", brand: "#dddddd", halo: "#cccccc", labelAgent: "#bbbbbb" },
    },
  };

  it("maps each decorative token onto its own variable", () => {
    apply(decorated, "light");
    expect(read("--link")).toBe("#111111");
    expect(read("--brand")).toBe("#222222");
    expect(read("--halo")).toBe("#333333");
    expect(read("--label-agent")).toBe("#444444");
    apply(decorated, "dark");
    expect(read("--link")).toBe("#eeeeee");
  });

  it("tints the washes from the decorative colour on the pack's own ground", () => {
    apply(decorated, "light");
    expect(read("--brand-wash")).toBe("color-mix(in srgb, #222222 12%, #FFFFFF)");
    expect(read("--label-agent-wash")).toBe("color-mix(in srgb, #444444 12%, #FFFFFF)");
  });

  it("leaves the washes on their defaults when the pack names no decorative colour", () => {
    apply(pack, "light");
    for (const name of ["--link", "--brand", "--halo", "--label-agent", "--brand-wash", "--label-agent-wash"]) {
      expect(read(name), name).toBe("");
    }
  });

  it("never writes a status variable, whatever the pack carries", () => {
    const hostile = {
      ...decorated,
      tokens: { light: { ...decorated.tokens.light, net: "#ff00ff", deleg: "#00ffff", ok: "#ff0000", err: "#00ff00", focus: "#ffff00" }, dark: {} },
    } as unknown as ThemePack;
    apply(hostile, "light");
    for (const name of ["--net", "--deleg", "--ok", "--warn", "--err", "--add", "--del", "--focus", "--net-wash", "--deleg-wash"]) {
      expect(read(name), name).toBe("");
    }
    expect(read("--link")).toBe("#111111");
  });
});

describe("a pack's recipes step the shape and the rhythm", () => {
  // A factor, not a size: the stylesheet owns what a radius or a gutter
  // measures, so a pack that named a number would disagree with every
  // declaration it does not reach. Held here because the pack is where the
  // wrong number would have to be written.
  it("writes the two factors and no measurement of its own", () => {
    apply({ ...pack, recipes: { corners: "square", density: "compact" } }, "light");
    expect(read("--r-scale")).toBe("0");
    expect(read("--d-scale")).toBe("0.85");
  });

  it("leaves both unset when the pack declares no recipe", () => {
    apply(pack, "light");
    expect(read("--r-scale")).toBe("");
    expect(read("--d-scale")).toBe("");
  });

  it("ignores a tier the schema does not publish", () => {
    const invented = { ...pack, recipes: { corners: "bevelled", density: "sparse" } } as unknown as ThemePack;
    apply(invented, "light");
    expect(read("--r-scale")).toBe("");
    expect(read("--d-scale")).toBe("");
  });

  it("steps each tier rather than pinning one value", () => {
    apply({ ...pack, recipes: { corners: "round", density: "comfortable" } }, "light");
    expect(read("--r-scale")).toBe("1.5");
    expect(read("--d-scale")).toBe("1");
    apply({ ...pack, recipes: { corners: "soft" } }, "light");
    expect(read("--r-scale")).toBe("1");
    expect(read("--d-scale")).toBe("");
  });
});
