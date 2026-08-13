// Run: tsx src/__tests__/typography-overflow-contract.test.ts

import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { JSDOM } from "jsdom";
import { TEXT_SIZES } from "../lib/textSize";

const testDir = dirname(fileURLToPath(import.meta.url));
const styles = [
  readFileSync(resolve(testDir, "../styles.css"), "utf8"),
  readFileSync(resolve(testDir, "../components/CompactRatioSettings.css"), "utf8"),
].join("\n").replace(/\/\*[\s\S]*?\*\//g, "");

let passed = 0;
let failed = 0;

function ok(value: unknown, label: string) {
  if (value) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}\n`);
    failed += 1;
  }
}

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

function hasDeclaration(selector: string, property: string, expected: string): boolean {
  return matchingBlocks(selector).some((block) => {
    const declaration = new RegExp(`(?:^|;)\\s*${property}\\s*:\\s*([^;]+)`, "g");
    let match: RegExpExecArray | null;
    while ((match = declaration.exec(block)) !== null) {
      if (match[1].trim() === expected) return true;
    }
    return false;
  });
}

function clipsSingleLine(selector: string) {
  eq(finalDeclaration(selector, "overflow"), "hidden", `${selector} clips long text`);
  eq(finalDeclaration(selector, "text-overflow"), "ellipsis", `${selector} uses ellipsis`);
  eq(finalDeclaration(selector, "white-space"), "nowrap", `${selector} stays on one line`);
}

console.log("\ntypography overflow contract");

eq(
  JSON.stringify(TEXT_SIZES),
  JSON.stringify(["small", "default", "large", "xlarge", "xxlarge"]),
  "text-size presets include the large accessibility step",
);
eq(finalDeclaration(":root", "--sans"), "var(--font-ui)", "legacy sans alias stays synced with UI font");
eq(finalDeclaration(':root[data-text-size="xxlarge"]', "--font-scale"), "1.32", "xxlarge has a real scale bump");
ok(
  (finalDeclaration(":root", "--statusbar-dock-height") ?? "").includes("var(--font-scale)"),
  "status bar dock height scales with interface text size",
);
ok(
  hasDeclaration(".layout", "--statusbar-height", "var(--statusbar-dock-height)"),
  "layout reserves scaled status bar height",
);
eq(
  finalDeclaration(".app", "height"),
  "var(--app-viewport-height, 100%)",
  "app height follows the live viewport height variable",
);
eq(finalDeclaration(".transcript--empty", "overflow-y"), "auto", "empty transcript can scroll instead of clipping");
eq(finalDeclaration(".welcome", "overflow"), "visible", "welcome empty state is not clipped by its own box");
ok(
  /\.md\s*>\s*:where\([^)]*p[^)]*ul[^)]*ol[^)]*\)\s*\{[^}]*content-visibility:\s*auto;[^}]*contain-intrinsic-size:\s*auto 72px;/.test(styles),
  "non-transcript markdown still culls offscreen blocks with a 72px placeholder",
);
ok(
  /\.transcript__row\s+\.md\s*>\s*\*\s*(?:,[^{]*)?\{[^}]*content-visibility:\s*visible;[^}]*contain-intrinsic-size:\s*none;/.test(styles),
  "virtual transcript rows do not measure markdown through 72px placeholders",
);
ok(
  hasDeclaration(".transcript__row .msg", "content-visibility", "visible") &&
    hasDeclaration(".transcript__row .turn-collapse", "content-visibility", "visible"),
  "virtual transcript cards stay measurable after the markdown override",
);

function paddingSides(value: string) {
  const parts = value.trim().split(/\s+/);
  if (parts.length === 1) return { right: parts[0], left: parts[0] };
  if (parts.length === 2 || parts.length === 3) return { right: parts[1], left: parts[1] };
  return { right: parts[1], left: parts[3] };
}
function isZeroPad(value: string | undefined) {
  return value === undefined || value === "0" || value === "0px";
}
for (const block of matchingBlocks(".transcript")) {
  const shorthand = /(?:^|;)\s*padding\s*:\s*([^;]+)/.exec(block);
  if (shorthand) {
    const sides = paddingSides(shorthand[1]);
    ok(
      isZeroPad(sides.left) && isZeroPad(sides.right),
      `Virtuoso scroller padding stays vertical-only (${shorthand[1].trim()})`,
    );
  }
  const padLeft = /(?:^|;)\s*padding-left\s*:\s*([^;]+)/.exec(block);
  const padRight = /(?:^|;)\s*padding-right\s*:\s*([^;]+)/.exec(block);
  ok(isZeroPad(padLeft?.[1].trim()), "Virtuoso scroller does not set padding-left");
  ok(isZeroPad(padRight?.[1].trim()), "Virtuoso scroller does not set padding-right");
}
ok(hasDeclaration(".transcript", "--transcript-inline-pad", "32px"), "default transcript inline inset is 32px");
ok(hasDeclaration(".transcript", "--transcript-inline-pad", "16px"), "narrow viewports tighten the transcript inline inset");
eq(finalDeclaration(".transcript__row", "padding-left"), "var(--transcript-inline-pad, 32px)", "virtual rows own the left inset");
eq(finalDeclaration(".transcript__row", "padding-right"), "var(--transcript-inline-pad, 32px)", "virtual rows own the right inset");
eq(finalDeclaration(".transcript__header", "padding-left"), "var(--transcript-inline-pad, 32px)", "load-older header uses the same inline inset");
eq(finalDeclaration(".transcript--empty", "padding"), "16px 32px", "empty transcript keeps its own horizontal inset");

{
  const stylesheet = readFileSync(resolve(testDir, "../styles.css"), "utf8");
  const dom = new JSDOM(
    `<!doctype html><html><head><style>${stylesheet}</style></head><body>
      <div class="transcript__row"><div class="md"><p id="inside">inside</p></div></div>
      <div class="md"><p id="outside">outside</p></div>
    </body></html>`,
    { pretendToBeVisual: true },
  );
  const inside = dom.window.getComputedStyle(dom.window.document.getElementById("inside")!);
  const outside = dom.window.getComputedStyle(dom.window.document.getElementById("outside")!);
  // jsdom may not implement content-visibility; treat an empty computed value
  // as "engine gap" and still require the source contract above.
  if (inside.contentVisibility || outside.contentVisibility) {
    eq(inside.contentVisibility, "visible", "computed style keeps transcript markdown measurable");
    eq(outside.contentVisibility, "auto", "computed style still culls markdown outside the transcript");
  }
  dom.window.close();
}
ok(
  hasDeclaration(".transcript--empty > .welcome", "margin-block", "auto"),
  "empty-state auto margins apply only to the welcome content",
);
ok(
  finalDeclaration(".transcript--empty > *", "margin-block") === undefined,
  "empty-state generic children do not receive auto margins",
);
eq(
  finalDeclaration(":root[data-theme-style] .statusbar", "height"),
  "var(--statusbar-dock-height)",
  "fixed status bar height follows the scaled dock token",
);
eq(
  finalDeclaration(":root[data-theme-style] .statusbar", "min-height"),
  "var(--statusbar-dock-height)",
  "status bar min-height follows the scaled dock token",
);
eq(finalDeclaration(".provider-template-grid", "grid-auto-rows"), "92px", "provider preset cards use compact equal-height grid rows");
eq(finalDeclaration(".provider-template-card", "height"), "100%", "provider preset cards stretch to the grid row height");
eq(finalDeclaration(".provider-template-card strong", "-webkit-line-clamp"), "1", "provider preset card titles clamp to one line");
eq(finalDeclaration(".provider-template-card span", "-webkit-line-clamp"), "2", "provider preset card descriptions clamp to two lines");
eq(finalDeclaration(".provider-model-draft__list", "grid-auto-rows"), "min-content", "provider model rows grow with their content");
eq(finalDeclaration(".provider-model-draft__option", "min-height"), undefined, "provider model cards do not force undersized rows");
eq(finalDeclaration(".provider-model-draft__option", "overflow"), "hidden", "provider model cards contain overflowing controls");
eq(finalDeclaration(".compact-ratio-presets", "width"), "100%", "compaction presets use the full settings control width");
eq(finalDeclaration(".compact-ratio-presets .set-seg__btn", "flex"), "1 1 0", "three compaction presets share the available width equally");
eq(finalDeclaration(".compact-ratio-presets .set-seg__btn", "flex-direction"), "column", "compaction presets place percentage and strategy on separate lines");
eq(finalDeclaration(".compact-ratio-presets .set-seg__btn", "min-height"), "44px", "two-line compaction presets keep a stable target height");
eq(finalDeclaration(".compact-ratio-presets .set-seg__btn", "white-space"), "normal", "compaction labels do not depend on ellipsis for their meaning");

eq(finalDeclaration(".statusbar", "white-space"), "nowrap", "status bar keeps metrics on one row");
eq(finalDeclaration(".statusbar", "overflow-y"), "hidden", "status bar hides vertical overflow");
clipsSingleLine(".statusbar__model");

for (const selector of [
  ".sidebar-im__summary-label",
  ".sidebar-im__summary-status",
  ".workbench-dock__tab-label",
  ".workspace-files__scope-title",
  ".workspace-files__scope-meta",
  ".context-panel__section-head span",
  ".context-panel__metric span",
  ".context-panel__metric strong",
  ".app--creation .context-panel__mini-stat span",
  ".app--creation .context-panel__mini-stat strong",
  ".topbar__model",
  ".composer-modebar__item span",
  ".composer-more-menu__item span",
]) {
  clipsSingleLine(selector);
}

eq(
  finalDeclaration(".app--creation .layout.layout--workspace-open", "transition"),
  "grid-template-columns 0s, min-width 0s",
  "creation dock skips zero-width grid interpolation on open",
);
eq(
  finalDeclaration(".app--creation .context-panel__usage", "animation"),
  "none",
  "creation overview usage card disables inherited entrance animation",
);
ok(
  finalDeclaration(".app--creation .context-panel__mini-stat", "justify-content") !== "space-between",
  "creation overview rows avoid edge-pinned value alignment",
);
ok(
  finalDeclaration(".app--creation .context-panel__mini-stat", "grid-template-columns") !== "minmax(0, 1fr) auto",
  "creation overview rows avoid the spacer grid that pushes values to the edge",
);
ok(
  finalDeclaration(".app--creation .context-panel__mini-stat strong", "max-width") !== "14ch",
  "creation overview values are not capped to a fixed 14ch width",
);

eq(finalDeclaration(".composer-modebar", "overflow"), "hidden", "chat mode switcher contains enlarged labels");
eq(finalDeclaration(".composer-meta__control--profile", "flex"), "0 0 auto", "work mode selector sizes to its localized label");
eq(finalDeclaration(".composer-meta__control--profile", "max-width"), "68px", "work mode selector keeps a compact narrow-width bound");
eq(finalDeclaration(".composer-profile-trigger__label", "overflow"), "hidden", "work mode selector clips only when space is constrained");
eq(finalDeclaration(".composer-profile-trigger__label", "text-overflow"), "ellipsis", "work mode selector shows an ellipsis when constrained");
eq(finalDeclaration(".composer-meta__control--intent", "max-width"), "72px", "task method selector keeps its current state visible at narrow widths");
eq(finalDeclaration(".composer-task-mode-trigger__value", "text-overflow"), "ellipsis", "task method selector truncates its value only when constrained");
eq(finalDeclaration(".composer-meta .modelsw__trigger", "font-weight"), "var(--composer-control-font-weight)", "model selector uses the shared control weight");
eq(finalDeclaration(".composer-meta__divider", "height"), "18px", "execution policy and model settings have a compact visual divider");
ok(
  /@container \(max-width: 560px\)\s*\{[\s\S]*?\.composer-meta__control--more\s*\{[\s\S]*?flex-basis:\s*38px;/.test(styles),
  "composer enters icon-only mode before model and effort controls overlap",
);
ok(
  /@container \(max-width: 760px\)\s*\{[\s\S]*?\.composer-meta__control--approval \.composer-modebar--approval\s*\{[^}]*flex:\s*1 1 auto;[^}]*width:\s*100%;[^}]*min-width:\s*0;[^}]*max-width:\s*100%;/.test(styles),
  "approval mode switcher shrinks with its compact composer container",
);
eq(finalDeclaration(".composer-modebar--approval", "--composer-modebar-active-bg"), "var(--mode-auto-bg)", "ask approval restores the solid semantic fill");
eq(finalDeclaration('.composer-modebar--approval[data-mode="auto"]', "--composer-modebar-active-fg"), "#fff", "auto approval keeps high-contrast text on its solid fill");
eq(finalDeclaration('.composer-modebar--approval[data-mode="yolo"]', "--composer-modebar-active-bg"), "var(--mode-yolo-bg)", "yolo approval restores the solid warning fill");
eq(finalDeclaration(".composer-intent-menu", "width"), "min(284px, calc(100vw - 16px))", "task method menu uses the shared menu width");
eq(finalDeclaration(".composer-profile-menu", "width"), "min(284px, calc(100vw - 16px))", "work mode menu uses the shared menu width");
eq(finalDeclaration(".composer-access-menu__desc", "white-space"), "normal", "menu descriptions can wrap onto a second line");
eq(finalDeclaration(".composer-access-menu__desc", "text-overflow"), "clip", "menu descriptions no longer use single-line ellipsis");
eq(finalDeclaration(".composer-profile-menu .composer-access-menu__desc", "font-size"), "12px", "work mode summaries use the shared control text size");
eq(finalDeclaration(".composer-profile-menu .composer-access-menu__desc", "color"), "var(--fg-dim)", "work mode summaries remain readable as secondary text");
eq(finalDeclaration(".composer-profile-menu .composer-access-menu__desc", "white-space"), "nowrap", "work mode summaries stay on one scannable line");
eq(finalDeclaration(".composer-task-mode-trigger:focus-visible", "box-shadow"), "var(--focus-ring)", "task method selector uses the shared keyboard focus ring");
eq(finalDeclaration(".composer-profile-trigger:focus-visible", "box-shadow"), "var(--focus-ring)", "work mode selector uses the shared keyboard focus ring");
eq(finalDeclaration(".composer-meta .modelsw__trigger:focus-visible", "box-shadow"), "var(--focus-ring)", "model and effort selectors use the shared keyboard focus ring");
eq(finalDeclaration(":root[data-theme-style] .composer-modebar__item--active:focus-visible", "box-shadow"), "var(--focus-ring)", "active permission options retain keyboard focus feedback");
eq(
  finalDeclaration(".app--creation .msg--assistant .msg__body", "font-size"),
  "var(--font-content)",
  "creation assistant body text follows the conversation text size",
);
eq(
  finalDeclaration(":root[data-theme-style] .msg--assistant .msg__body", "font-size"),
  "var(--font-content)",
  "themed assistant body text follows the conversation text size",
);
eq(
  finalDeclaration(".app--creation .msg--assistant .msg__body", "font-family"),
  "var(--font-content-family)",
  "creation assistant body follows the conversation font family",
);
eq(
  finalDeclaration(".app--creation .md", "font-family"),
  "var(--font-content-family)",
  "creation markdown follows the conversation font family",
);
eq(
  finalDeclaration(".app--creation .composer__input", "font-size"),
  "var(--font-content)",
  "creation composer input follows the composer text size",
);
eq(
  finalDeclaration("body", "--text-base"),
  "var(--typography-interface-size, calc(14px * var(--font-scale)))",
  "interface text resolves its own exact regional size",
);
eq(
  finalDeclaration(".transcript", "--text-base"),
  "var(--typography-conversation-size, calc(14px * var(--font-scale)))",
  "conversation text resolves its own exact regional size",
);
eq(
  finalDeclaration(".composer-wrap", "--text-base"),
  "var(--typography-composer-size, calc(14px * var(--font-scale)))",
  "composer text resolves its own exact regional size",
);
eq(
  finalDeclaration(".code", "--font-code"),
  "var(--typography-code-size, calc(12px * var(--font-scale)))",
  "code text resolves its own exact regional size",
);
eq(finalDeclaration(".code", "font-family"), "var(--font-code-family)", "code blocks keep the regional code font");
eq(finalDeclaration(".md-code", "font-family"), "var(--font-code-family)", "inline code keeps the regional code font");
eq(finalDeclaration(".code code", "font-family"), "inherit", "nested code text inherits the regional code font");
eq(finalDeclaration(".code-line-text", "font-family"), "inherit", "line-numbered code text inherits its viewer font");
eq(
  finalDeclaration(".code-lines-wrap", "font-family"),
  "var(--typography-code-font, var(--font-mono))",
  "line-numbered code viewers use the regional code font",
);
eq(
  finalDeclaration(".diff", "font-size"),
  "var(--typography-code-size, calc(12.5px * var(--global-font-scale)))",
  "diff text follows the global scale until the code region is customized",
);
eq(finalDeclaration(".msg-meta", "font-size"), "var(--font-status)", "message metadata keeps its regional size");
eq(
  finalDeclaration(".composer-meta", "font-family"),
  "var(--font-metadata-family)",
  "composer metadata keeps the regional font",
);
eq(finalDeclaration(".statusbar", "font-family"), "var(--font-metadata-family)", "status bar keeps the regional font");
eq(
  finalDeclaration(".typography-settings__preview", "--preview-size"),
  "var(--typography-conversation-size, calc(14px * var(--global-font-scale)))",
  "conversation preview uses the exact conversation size",
);
eq(
  finalDeclaration(".typography-settings__preview--interface", "--preview-size"),
  "var(--typography-interface-size, calc(14px * var(--global-font-scale)))",
  "interface preview uses the exact interface size",
);
eq(
  finalDeclaration(".typography-settings__preview--composer", "--preview-size"),
  "var(--typography-composer-size, calc(14px * var(--global-font-scale)))",
  "composer preview uses the exact composer size",
);
eq(
  finalDeclaration(".typography-settings__preview--code", "--preview-size"),
  "var(--typography-code-size, calc(12px * var(--global-font-scale)))",
  "code preview uses the exact code size",
);
eq(
  finalDeclaration(".typography-settings__preview--metadata", "--preview-size"),
  "var(--typography-metadata-size, calc(12px * var(--global-font-scale)))",
  "metadata preview uses the exact supporting-text size",
);
eq(
  finalDeclaration(".typography-settings__preview-body", "font-size"),
  "var(--preview-size)",
  "live preview renders the selected region's exact size",
);
eq(
  finalDeclaration(".app--creation .reasoning__body", "font-family"),
  "var(--font-content-family)",
  "creation reasoning keeps the conversation font",
);
eq(
  finalDeclaration(".app--creation .tool__name", "font-family"),
  "var(--font-code-family)",
  "creation tool names keep the code font",
);
ok(
  !/\.app--creation[^{]*\{[^}]*font-size:\s*[0-9.]+px\s*(?:!important\s*)?;/.test(styles),
  "creation rules do not hardcode bare px font sizes (except font-size:0)",
);
eq(
  finalDeclaration(".context-ring-popover__title", "font-size"),
  "calc(14px * var(--font-scale))",
  "creation context-ring popover (portaled to body) follows interface text size",
);
ok(
  !/\.context-ring-popover[^{]*\{[^}]*font-size:\s*[0-9.]+px\s*(?:!important\s*)?;/.test(styles),
  "context-ring popover rules do not hardcode bare px font sizes (except font-size:0)",
);
eq(
  finalDeclaration(".app--creation .tool:not(.tool--open) > .tool__body", "height"),
  "0 !important",
  "collapsed creation tool bodies keep mounted content clipped",
);
eq(
  finalDeclaration(".app--creation .tool:not(.tool--open) > .tool__body", "visibility"),
  "hidden",
  "collapsed creation tool bodies do not paint hidden tool text",
);
ok(
  /@container\s*\(max-width:\s*760px\)[\s\S]*?\.composer-meta__control--model\s*\{[\s\S]*?flex\s*:\s*0 1 auto[\s\S]*?width\s*:\s*fit-content[\s\S]*?max-width\s*:\s*min\(240px,\s*42vw\)[\s\S]*?\.composer-meta__control--profile\s*\{[\s\S]*?max-width\s*:\s*126px[\s\S]*?\.composer-meta__control--intent\s*\{[\s\S]*?max-width\s*:\s*128px[\s\S]*?\.composer-meta__control--effort\s*\{[\s\S]*?display\s*:\s*none[\s\S]*?\.composer-meta__control--more\s*\{[\s\S]*?display\s*:\s*inline-flex/.test(styles),
  "composer compact controls activate at the capped theme width",
);
eq(finalDeclaration(".md-table-scroll", "overflow-x"), "auto", "markdown table wrapper scrolls horizontally");
eq(finalDeclaration(".md-table-scroll", "overflow-y"), "hidden", "markdown table wrapper does not nest vertical scroll");
eq(finalDeclaration(".md table", "overflow"), "visible", "markdown tables stay in document flow for trackpad Y");
eq(finalDeclaration(".md-table-fold", "display"), "flex", "large tables use a fold stack for preview + expand");
eq(finalDeclaration(".md-table-fold__toggle", "cursor"), "pointer", "table expand control is clickable");
eq(finalDeclaration(".code", "overflow-x"), "auto", "code blocks scroll horizontally instead of widening the layout");
eq(finalDeclaration(".code", "overflow-y"), "hidden", "code blocks do not nest vertical scroll by default");
ok(
  /@media\s*\(max-width:\s*900px\)[\s\S]*?\.settings-center\s*\{[\s\S]*?grid-template-columns\s*:\s*1fr/.test(styles),
  "settings center stacks navigation before the modal is too narrow",
);
ok(
  /@media\s*\(max-width:\s*900px\)[\s\S]*?\.settings-field\s*\{[\s\S]*?grid-template-columns\s*:\s*1fr/.test(styles),
  "settings fields collapse to one column at the mid-width breakpoint",
);
ok(
  /@media\s*\(max-width:\s*760px\)[\s\S]*?\.settings-modal\s*\{[\s\S]*?width\s*:\s*100vw[\s\S]*?height\s*:\s*100vh/.test(styles),
  "settings modal only becomes fullscreen at the narrow breakpoint",
);
ok(
  /@media\s*\(max-width:\s*820px\)[\s\S]*?\.app\s+\.layout[\s\S]*?grid-template-columns\s*:\s*minmax\(0,\s*1fr\)\s*!important[\s\S]*?\.app\s+\.sidebar[\s\S]*?display\s*:\s*none\s*!important[\s\S]*?\.app\s+\.chat-pane[\s\S]*?grid-column\s*:\s*1\s*!important/.test(styles),
  "narrow workbench layout hides side panels and keeps chat single-column",
);

for (const selector of [
  ".reasoning__head",
  ".turn-collapse__reasoning-head",
  ".process-card__head",
  ".tool__difflabel",
  ".msg-memory-citations",
  ".msg-memory-citations__source",
  ".msg-memory-citations__note",
  ".msg-attachment__name",
  ".msg-attachment__meta",
  ".msg-pasted-head",
  ".msg-pasted-expanded",
  ".msg-edit__input",
  ".msg-edit__btn",
  ".msg__send-failed",
  ":root[data-theme-style] .process-card__kind",
  ':root[data-theme-style] .msg--assistant > .process-card[data-tone="violet"] .process-card__name',
]) {
  const size = finalDeclaration(selector, "font-size");
  ok(size !== undefined && !/^[0-9.]+px$/.test(size), `${selector} font size follows the text-size scale`);
}

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
