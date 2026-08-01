// Run: tsx src/__tests__/footer-decision-overflow.test.ts
//
// Contract: the footer becomes a scroll/clip container ONLY while a decision
// surface (plan/tool approval, ask, clear-context) is shown. Scoping the
// overflow to `.footer--decision` keeps tall approval cards scrolling inside a
// capped footer (#7030) while leaving the composer state unclipped, so the
// upward-popping "/" and "@" menus (.slashmenu — position:absolute; bottom:
// calc(100% + 6px)) can expand to full height. An unconditional overflow-y on
// .footer clipped those menus to a thin strip above the input box (regression
// shipped after desktop-v1.18.0 via the #7030 fix in #7069).

import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const testDir = dirname(fileURLToPath(import.meta.url));
// Strip comments so declaration parsing never matches prose inside them.
const styles = readFileSync(resolve(testDir, "../styles.css"), "utf8").replace(/\/\*[\s\S]*?\*\//g, "");
const appSource = readFileSync(resolve(testDir, "../App.tsx"), "utf8");

let passed = 0;
let failed = 0;

function eq(a: unknown, b: unknown, label: string) {
  if (a === b) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}: expected ${JSON.stringify(b)}, got ${JSON.stringify(a)}\n`);
    failed += 1;
  }
}

function matchingBlocks(selector: string): string[] {
  const blocks: string[] = [];
  const rule = /([^{}]+)\{([^{}]*)\}/g;
  let match: RegExpExecArray | null;
  while ((match = rule.exec(styles)) !== null) {
    const selectors = match[1].split(",").map((part) => part.trim());
    if (selectors.includes(selector)) blocks.push(match[2]);
  }
  return blocks;
}

function finalDeclaration(selector: string, property: string): string | undefined {
  let value: string | undefined;
  for (const block of matchingBlocks(selector)) {
    const declaration = new RegExp(`(?:^|;)\\s*${property}\\s*:\\s*([^;]+)`, "g");
    let match: RegExpExecArray | null;
    while ((match = declaration.exec(block)) !== null) {
      value = match[1].trim();
    }
  }
  return value;
}

console.log("\nfooter decision overflow");

// AC1 — composer state: the base footer is not a scroll/clip container, so the
// upward-popping menus are free to expand.
eq(finalDeclaration(".footer", "overflow-y"), undefined, "base .footer does not scroll/clip (composer menus can expand)");
eq(finalDeclaration(".footer", "max-height"), undefined, "base .footer is not height-capped in the composer state");
eq(finalDeclaration(".footer", "flex"), "0 0 auto", "base .footer keeps its flex sizing");

// AC2 — decision state: capped + scrollable so tall approval cards stay
// reachable and never slip behind the status bar (#7030 stays fixed).
eq(finalDeclaration(".footer--decision", "overflow-y"), "auto", "decision footer scrolls tall approval cards");
eq(finalDeclaration(".footer--decision", "max-height"), "calc(100% - var(--topicbar-height))", "decision footer is capped above the status bar");

// AC3 — the menu contract itself is untouched by this fix.
eq(finalDeclaration(".slashmenu", "position"), "absolute", ".slashmenu still anchors to the composer");
eq(finalDeclaration(".slashmenu", "bottom"), "calc(100% + 6px)", ".slashmenu still pops above the composer");
eq(finalDeclaration(".slashmenu", "max-height"), "min(360px, 50vh)", ".slashmenu keeps its full pop height");

// AC4 — App.tsx applies footer--decision exactly when a decision surface owns
// the footer, and the pre-existing footer--compact wiring stays intact.
eq(/decisionSurface \? "footer--decision" : ""/.test(appSource), true, "App.tsx toggles footer--decision on decisionSurface");
eq(/terminalPanelOpen && !sidebarCreation \? "footer--compact" : ""/.test(appSource), true, "footer--compact wiring stays intact");

// AC5 — mutual-exclusivity guard: the composer host is hidden while a decision
// owns the footer, so a menu can never open in the decision state.
eq(finalDeclaration(".composer-decision-host--hidden", "display"), "none !important", "composer host stays hidden under a decision (menus cannot open)");

// AC6 — guard the detection gap noted in #7128's review: the exact-match
// helpers above only see selectors equal to `.footer` / `.footer--decision`, so
// ancestor-qualified or combined variants (`:root[data-theme-style] .footer`,
// `.app--creation .footer`, `.footer.footer--compact`, …) go unchecked. No
// footer rule other than the decision modifier may turn the footer into a
// clip/scroll container, or the upward-popping composer menus get clipped again.
// Matching is scoped to each selector's rightmost (target) compound, so rules
// that merely descend from `.footer` (e.g. `.footer .child`) are not misread as
// footer rules, and the decision exemption applies only to the parts that
// actually target the footer element.

// The element a selector styles is its rightmost compound — the segment after
// the last combinator. An ancestor `.footer` (e.g. `.footer .child`) styles the
// child, not the footer, so only that rightmost compound decides whether a rule
// targets the footer element. `.footer__*` BEM children are not the footer.
function footerTarget(selectorPart: string): string {
  return selectorPart.split(/[\s>+~]+/).pop() ?? "";
}

function targetsFooterElement(selectorPart: string): boolean {
  return /\.footer(?!__)(--[a-z-]+)?(?=$|[.:\[])/.test(footerTarget(selectorPart));
}

// True if the part targets the footer through the decision modifier — the one
// state allowed to turn the footer into a scroll/clip container.
function isDecisionTarget(selectorPart: string): boolean {
  return footerTarget(selectorPart).includes(".footer--decision");
}

function declValue(body: string, property: string): string | undefined {
  const declaration = new RegExp(`(?:^|;)\\s*${property}\\s*:\\s*([^;]+)`, "g");
  let value: string | undefined;
  let match: RegExpExecArray | null;
  while ((match = declaration.exec(body)) !== null) value = match[1].trim();
  return value;
}

function footerClipOffenders(): string[] {
  const offenders: string[] = [];
  const rule = /([^{}]+)\{([^{}]*)\}/g;
  let match: RegExpExecArray | null;
  while ((match = rule.exec(styles)) !== null) {
    const selectorList = match[1].trim();
    const parts = selectorList.split(",").map((part) => part.trim());
    const footerParts = parts.filter(targetsFooterElement);
    if (footerParts.length === 0) continue;
    if (footerParts.every(isDecisionTarget)) continue;
    const body = match[2];
    const clips =
      (declValue(body, "overflow") ?? "visible") !== "visible" ||
      (declValue(body, "overflow-y") ?? "visible") !== "visible" ||
      (declValue(body, "overflow-x") ?? "visible") !== "visible";
    if (clips || declValue(body, "max-height") !== undefined) offenders.push(selectorList);
  }
  return offenders;
}

eq(footerClipOffenders().join(", "), "", "no footer rule except .footer--decision clips or caps the footer");

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
