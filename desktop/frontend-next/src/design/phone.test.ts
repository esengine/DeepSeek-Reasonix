import { describe, expect, it } from "vitest";
import phone from "./phone.css?raw";
import index from "./design.css?raw";
import narrow from "../styles/narrow.css?raw";
import studio from "../styles/studio.css?raw";

const rule = (selector: string): string => {
  const at = phone.indexOf(selector + " {");
  return at < 0 ? "" : phone.slice(at, phone.indexOf("}", at));
};

describe("the phone's title bar", () => {
  it("is part of the design layer", () => {
    expect(index).toMatch(/@import "\.\/phone\.css" layer\(design\)/);
  });

  it("puts the session list at the left edge at the board's 28px, and leaves room for it", () => {
    const menu = rule('[data-fold~="phone"] .studio-menu');
    expect(menu).toMatch(/left:\s*8px/);
    expect(menu).toMatch(/width:\s*28px/);
    expect(menu).toMatch(/height:\s*28px/);
    expect(rule('[data-fold~="phone"] .chrome')).toMatch(/padding:\s*6px 8px 8px 42px/);
  });

  it("stacks a centred title over the machine line", () => {
    expect(rule('[data-fold~="phone"] .chrome .crumb')).toMatch(/flex-direction:\s*column/);
    expect(rule('[data-fold~="phone"] .chrome .crumb')).toMatch(/text-align:\s*center/);
    expect(rule('[data-fold~="phone"] .chrome .crumb > b')).toMatch(/font:\s*var\(--w-semi\) var\(--fs-lead\)/);
    expect(rule('[data-fold~="phone"] .chrome .crumb-sub')).toMatch(/color:\s*var\(--faint\)/);
  });

  it("colours the machine's dot by state, and only a running turn breathes", () => {
    expect(rule(".chrome .crumb-sub > i")).toMatch(/background:\s*var\(--ok\)/);
    expect(rule('.chrome .crumb-sub[data-state="run"] > i')).toMatch(/background:\s*var\(--net\)/);
    expect(rule('.chrome .crumb-sub[data-state="run"] > i')).toMatch(/animation:\s*ph-run 1\.4s/);
    expect(rule('.chrome .crumb-sub[data-state="down"] > i')).toMatch(/background:\s*var\(--warn\)/);
    expect(rule('.chrome .crumb-sub[data-state="down"] > i')).not.toMatch(/animation/);
    expect(phone).toMatch(/@keyframes ph-run\s*\{\s*0%, 100%\s*\{\s*opacity:\s*\.35/);
  });

  it("stops breathing under reduced motion, the system's or the app's", () => {
    expect(phone).toMatch(/prefers-reduced-motion: reduce\)[^}]*\.crumb-sub[^{]*\{[^}]*animation:\s*none/);
    expect(phone).toMatch(/:root\[data-effects="reduced"\] \.chrome \.crumb-sub[^{]*\{[^}]*animation:\s*none/);
  });
});

describe("the lost-connection banner on a phone", () => {
  it("turns warm and says it is reconnecting", () => {
    const down = rule('[data-fold~="phone"] .devicebar[data-down]');
    expect(down).toMatch(/color-mix\(in srgb, var\(--warn\) 14%, var\(--surface\)\)/);
    expect(rule('[data-fold~="phone"] .devicebar[data-down] .db-retry')).toMatch(/display:\s*inline/);
  });

  it("keeps the note and the spinner out of the connected bar", () => {
    expect(rule(".devicebar .db-retry, .devicebar .db-spin")).toMatch(/display:\s*none/);
  });
});

describe("the menu the bar's controls gather into", () => {
  it("collapses on a phone the same way it does in a very narrow window", () => {
    const collapse = narrow.split("\n").find((l) => l.includes(".chrome .r-fold {") && l.includes("position: absolute")) ?? "";
    expect(collapse).toMatch(/data-fold~="phone"/);
    const opened = narrow.split("\n").find((l) => l.includes(".r-fold[data-open]")) ?? "";
    expect(opened).toMatch(/data-fold~="phone"/);
  });
});

describe("the approval card on a phone", () => {
  const footer = '[data-fold~="phone"] .flow .call[data-approval] .apv-ft';

  it("is two equal columns of finger-sized buttons", () => {
    expect(rule(footer)).toMatch(/grid-template-columns:\s*1fr 1fr/);
    expect(rule(footer)).toMatch(/gap:\s*8px/);
    const button = rule(footer + " .btn");
    expect(button).toMatch(/height:\s*44px/);
    expect(button).toMatch(/border-radius:\s*var\(--r-md\)/);
  });

  it("puts refuse on the left in the error colour and allow on the right in the accent", () => {
    expect(rule(footer + " .btn[data-deny]")).toMatch(/order:\s*0/);
    expect(rule(footer + " .btn[data-deny]")).toMatch(/color:\s*var\(--err-ink\)/);
    expect(rule(footer + " .btn[data-primary]")).toMatch(/order:\s*1/);
    expect(rule(footer + " .btn[data-primary]")).toMatch(/background:\s*var\(--accent\)/);
    expect(rule(footer + " .btn[data-primary]")).toMatch(/color:\s*var\(--accent-fg\)/);
  });

  it("keeps the other choices behind 'more options' until they are asked for", () => {
    expect(rule(footer + " .btn:not([data-primary], [data-deny])")).toMatch(/display:\s*none/);
    expect(rule(footer + "[data-more] .btn:not([data-primary], [data-deny])")).toMatch(/display:\s*grid/);
    expect(rule(".apv-more")).toMatch(/display:\s*none/);
    expect(rule(footer + " .apv-more")).toMatch(/color:\s*var\(--link\)/);
  });
});

describe("the composer's tool row on a phone", () => {
  const row = '[data-fold~="phone"] .compose .turntools';

  it("sizes add as a 32px round button with a hairline", () => {
    const add = rule(row + " > .studio-attach:first-child");
    expect(add).toMatch(/width:\s*32px/);
    expect(add).toMatch(/height:\s*32px/);
    expect(add).toMatch(/border-radius:\s*50%/);
    expect(add).toMatch(/border:\s*1px solid var\(--border\)/);
  });

  it("draws the model as a 28px pill in the muted interface font", () => {
    const pill = rule(row + " .model-picker");
    expect(pill).toMatch(/height:\s*28px/);
    expect(pill).toMatch(/border-radius:\s*999px/);
    expect(pill).toMatch(/padding:\s*0 10px/);
    expect(pill).toMatch(/color:\s*var\(--muted\)/);
    expect(pill).toMatch(/font:\s*var\(--w-reg\) var\(--fs-meta\)/);
  });

  it("keeps everything else in the row behind the 'more' button until it is opened", () => {
    expect(rule(row + " > :not(:first-child, .studio-model-group, .turn-more)")).toMatch(/display:\s*none/);
    expect(rule(row + "[data-more] > :not(:first-child, .studio-model-group, .turn-more)")).toMatch(/display:\s*(flex|inline-flex)/);
    expect(rule(".turntools .turn-more")).toMatch(/display:\s*none/);
    expect(rule(row + " > .turn-more")).toMatch(/display:\s*inline-grid/);
  });

  it("makes send a 36px accent circle, and stop the same size", () => {
    const both = rule('[data-fold~="phone"] .compose .go .btn.send, [data-fold~="phone"] .compose .go .btn.stop');
    expect(both).toMatch(/width:\s*36px/);
    expect(both).toMatch(/height:\s*36px/);
    expect(both).toMatch(/border-radius:\s*50%/);
    expect(rule('\n[data-fold~="phone"] .compose .go .btn.send:not(:disabled)')).toMatch(/background:\s*var\(--accent\)/);
    expect(rule('\n[data-fold~="phone"] .compose .go .btn.stop')).toMatch(/background:\s*var\(--overlay\)/);
  });

  it("drops the keyboard hints, which a touch screen has no keys for", () => {
    expect(rule('[data-fold~="phone"] .compose .composecaps')).toMatch(/display:\s*none/);
  });
});

// An older rule pins the add button's order with !important, so it is scoped
// away from a phone and the phone's own order wins by source order.
describe("the order of the phone's tool row", () => {
  const row = '[data-fold~="phone"] .compose .turntools';

  it("runs add, model, the more button, then a line break, then the revealed controls", () => {
    expect(rule(row + " > :first-child")).toMatch(/order:\s*0;/);
    expect(rule(row + " > .studio-model-group")).toMatch(/order:\s*1;/);
    expect(rule(row + " > .turn-more")).toMatch(/order:\s*2;/);
    expect(rule(row + "[data-more]::after")).toMatch(/flex:\s*0 0 100%/);
    expect(rule(row + "[data-more]::after")).toMatch(/order:\s*3;/);
    expect(rule(row + " > :not(:first-child, .studio-model-group, .turn-more)")).toMatch(/order:\s*4;/);
  });

  it("is not fought by an !important order on a phone", () => {
    expect(phone).not.toMatch(/!important/);
    const pinned = studio.split("\n").filter((l) => /\.turntools > [^{]*\{[^}]*order:[^;]*!important/.test(l));
    expect(pinned.length).toBeGreaterThanOrEqual(6);
    for (const line of pinned) expect(line).toMatch(/^:root:not\(\[data-fold~="phone"\]\) /);
  });
});

describe("the phone stylesheets", () => {
  it("carry no control characters: an escape that was meant as a character is not a NUL", () => {
    expect(phone).not.toMatch(/[\u0000-\u0008\u000b\u000c\u000e-\u001f]/);
  });

  it("separates the model name from its reasoning level with a middle dot", () => {
    expect(rule('[data-fold~="phone"] .compose .turntools .model-picker .studio-model-sep::before')).toContain('content: "·"');
  });
});

describe("the composer's more-tools button", () => {
  const more = '[data-fold~="phone"] .compose .turntools > .turn-more svg';

  it("draws its three dots heavy enough to be seen, as the title bar's does", () => {
    const rule_ = rule(more);
    expect(rule_).toMatch(/stroke-width:\s*2\.6/);
    expect(rule_).toMatch(/width:\s*16px/);
  });

  it("is a ring like the add button beside it, so it reads as something to press", () => {
    const button = rule('[data-fold~="phone"] .compose .turntools > .turn-more');
    expect(button).toMatch(/border:\s*1px solid var\(--border\)/);
    expect(button).toMatch(/width:\s*32px/);
  });
});

describe("the model pill on a phone", () => {
  const row = '[data-fold~="phone"] .compose .turntools';

  it("takes the room its text needs instead of the narrow cap a small container gives it", () => {
    expect(rule(row + " .studio-model-control")).toMatch(/max-width:\s*none/);
    expect(rule(row + " .model-picker")).toMatch(/max-width:\s*none/);
  });

  it("shows the reasoning level after the dot, so the dot has something to separate", () => {
    expect(rule(row + " .model-picker .studio-effort-reading")).toMatch(/display:\s*inline-flex/);
  });
});
