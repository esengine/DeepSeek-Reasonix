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
const composerSource = readFileSync(resolve(testDir, "../components/Composer.tsx"), "utf8");

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

// AC3 — the menu stays anchored above the composer, but its preferred 360px cap
// is now bounded by the real viewport space measured by Composer.
eq(finalDeclaration(".slashmenu", "position"), "absolute", ".slashmenu still anchors to the composer");
eq(finalDeclaration(".slashmenu", "bottom"), "calc(100% + 6px)", ".slashmenu still pops above the composer");
eq(finalDeclaration(".slashmenu", "max-height"), "min(360px, var(--composer-menu-available-height, 50vh))", ".slashmenu uses the measured available height");

// AC4 — App.tsx applies footer--decision exactly when a decision surface owns
// the footer, and the pre-existing footer--compact wiring stays intact.
eq(/decisionSurface \? "footer--decision" : ""/.test(appSource), true, "App.tsx toggles footer--decision on decisionSurface");
eq(/terminalPanelOpen && !sidebarCreation \? "footer--compact" : ""/.test(appSource), true, "footer--compact wiring stays intact");
eq(/ref=\{composerWrapRef\}/.test(composerSource), true, "Composer measures the real menu anchor");
eq(/observeComposerMenuViewport\(anchor\)/.test(composerSource), true, "Composer observes geometry while a menu is open");

// AC5 — mutual-exclusivity guard: the composer host is hidden while a decision
// owns the footer, so a menu can never open in the decision state.
eq(finalDeclaration(".composer-decision-host--hidden", "display"), "none !important", "composer host stays hidden under a decision (menus cannot open)");

// AC6 — guard the detection gap noted in #7128's first review and covered by
// #7138: no scoped footer variant except the decision modifier may reintroduce
// clipping or a height cap around the composer menus.
function footerTarget(selectorPart: string): string {
  return selectorPart.split(/[\s>+~]+/).pop() ?? "";
}

function targetsFooterElement(selectorPart: string): boolean {
  return /\.footer(?!__)(--[a-z-]+)?(?=$|[.:\[])/.test(footerTarget(selectorPart));
}

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
    if (footerParts.length === 0 || footerParts.every(isDecisionTarget)) continue;
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

// AC7 — repeated pasted blocks and attachment rows stay bounded inside the
// composer footer. Pasted blocks must not flex-shrink vertically, otherwise
// the capped list compresses each block instead of becoming scrollable.
eq(finalDeclaration(".composer__pasted", "max-height"), "min(30vh, 240px)", "pasted block list is viewport-bounded");
eq(finalDeclaration(".composer__pasted", "overflow-y"), "auto", "pasted block list scrolls internally");
eq(finalDeclaration(".composer__pasted-block", "flex-shrink"), "0", "pasted blocks keep their readable height");
eq(finalDeclaration(".composer-context", "max-height"), "min(30vh, 200px)", "attachment list is viewport-bounded");
eq(finalDeclaration(".composer-context", "overflow-y"), "auto", "attachment list scrolls internally");

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
