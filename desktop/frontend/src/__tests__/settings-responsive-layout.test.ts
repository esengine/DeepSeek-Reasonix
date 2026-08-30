// Run: tsx src/__tests__/settings-responsive-layout.test.ts

import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const testDir = dirname(fileURLToPath(import.meta.url));
const styles = readFileSync(resolve(testDir, "../styles.css"), "utf8");
const panelStyles = readFileSync(resolve(testDir, "../components/SettingsPanel.css"), "utf8");

let passed = 0;
let failed = 0;

function eq(actual: unknown, expected: unknown, label: string) {
  if (actual === expected) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}: expected ${JSON.stringify(expected)}, got ${JSON.stringify(actual)}\n`);
    failed += 1;
  }
}

function ruleBlock(source: string, selector: string): string {
  const start = source.indexOf(`${selector} {`);
  if (start < 0) return "";
  const bodyStart = source.indexOf("{", start) + 1;
  const end = source.indexOf("}", bodyStart);
  return source.slice(bodyStart, end);
}

function declaration(block: string, property: string): string | undefined {
  const match = block.match(new RegExp(`(?:^|;)\\s*${property}\\s*:\\s*([^;]+)`));
  return match?.[1].trim();
}

console.log("\nsettings responsive layout contract");

const settingsModal = ruleBlock(panelStyles, ".settings-modal");
eq(declaration(settingsModal, "width"), "min(1380px, calc(100vw - clamp(32px, calc(96px - (900px - 100vw) * 0.457), 96px)))", "wide settings modal keeps centered desktop margins");
eq(declaration(settingsModal, "height"), "min(960px, calc(100vh - 80px))", "wide settings modal keeps the intended desktop height without becoming full screen");
const settingsCenter = ruleBlock(panelStyles, ".settings-center");
eq(declaration(settingsCenter, "grid-template-columns"), "clamp(220px, 20.5vw, 304px) minmax(0, 1fr)", "settings navigation remains readable without consuming the content pane");

const generalPage = ruleBlock(panelStyles, ".settings-page--general");
eq(declaration(generalPage, "container"), "settings-general / inline-size", "general settings respond to their available content width");

const generalContainerStart = panelStyles.indexOf("@container settings-general (max-width: 620px)");
const generalFallbackStart = panelStyles.indexOf("@media (max-width: 980px)", generalContainerStart);
const generalContainer = panelStyles.slice(generalContainerStart, generalFallbackStart);
const compactGeneralField = ruleBlock(generalContainer, ".settings-page--general .settings-field");
eq(declaration(compactGeneralField, "grid-template-columns"), "1fr", "general settings stack before controls overflow their content pane");
eq(declaration(compactGeneralField, "gap"), "10px", "stacked general settings keep compact vertical spacing");
const compactGeneralControl = ruleBlock(generalContainer, ".settings-page--general .settings-field__control");
eq(declaration(compactGeneralControl, "min-width"), "0", "compact general controls may shrink within the content pane");

const soundContainerStart = panelStyles.indexOf("@container settings-general (max-width: 440px)");
const soundContainerEnd = panelStyles.indexOf("@media (max-width: 980px)", soundContainerStart);
const compactSound = panelStyles.slice(soundContainerStart, soundContainerEnd);
const compactSoundRow = ruleBlock(compactSound, ".settings-sound-row");
eq(declaration(compactSoundRow, "grid-template-columns"), "minmax(0, 1fr)", "narrow sound settings stack labels above their controls");
eq(
  compactSound.includes(".settings-sound-row .notification-volume-control") && compactSound.includes("width: 100%"),
  true,
  "notification volume expands without overflowing a narrow settings pane",
);

const fallbackPanel = panelStyles.slice(generalFallbackStart, panelStyles.indexOf("@media (max-width: 760px)", generalFallbackStart));
const fallbackGeneralField = ruleBlock(fallbackPanel, ".settings-page--general .settings-field");
eq(declaration(fallbackGeneralField, "grid-template-columns"), "1fr", "980px fallback stacks general settings without container queries");

const generalSectionBoundary = ruleBlock(panelStyles, ".settings-page--general > .settings-section:not(:last-child) .settings-section__body");
eq(
  declaration(generalSectionBoundary, "border-bottom"),
  "1px solid var(--border-soft)",
  "general settings end intermediate sections with a subtle divider",
);

const wideTail = ruleBlock(styles, ".memory-tabs-row__tail");
eq(declaration(wideTail, "display"), "contents", "wide memory controls preserve the original row flex items");
eq(declaration(wideTail, "flex"), undefined, "wide memory wrapper does not compete in flex sizing");

const settingsResponsiveStart = styles.indexOf("/* ── responsive: compact content on narrow screens");
const narrowStart = styles.indexOf("@media (max-width: 900px)", settingsResponsiveStart);
const narrowEnd = styles.indexOf("@media (max-width: 760px)", narrowStart);
const narrowSettings = styles.slice(narrowStart, narrowEnd);
const narrowTail = ruleBlock(narrowSettings, ".memory-tabs-row__tail");

eq(declaration(narrowTail, "display"), "flex", "narrow memory controls form a flex group");
eq(declaration(narrowTail, "flex"), "0 1 100%", "narrow memory controls stay on their own row");
eq(declaration(narrowTail, "min-width"), "0", "narrow memory controls may shrink without overflowing");
eq(declaration(narrowTail, "gap"), "12px", "narrow memory controls retain their compact spacing");

const narrowModalOverrideSelector = ".settings-modal.management-modal,\n  :root[data-theme-style] .settings-modal.management-modal";
const narrowModalOverride = ruleBlock(panelStyles, narrowModalOverrideSelector);
eq(declaration(narrowModalOverride, "width"), "100vw", "minimum-width settings modal spans the viewport");
eq(declaration(narrowModalOverride, "height"), "100vh", "minimum-width settings modal uses the full viewport height");
eq(declaration(narrowModalOverride, "max-height"), "100vh", "minimum-width settings modal clears the shared modal height cap");
eq(declaration(narrowModalOverride, "border-radius"), "0", "minimum-width settings modal removes floating-panel corners");
eq(
  panelStyles.indexOf(narrowModalOverrideSelector) > panelStyles.indexOf("@media (max-width: 760px)"),
  true,
  "minimum-width settings modal override follows the shared theme shell",
);

const compactPanelStart = panelStyles.indexOf("@media (max-width: 760px)");
const compactPanel = panelStyles.slice(compactPanelStart);
const compactCenter = ruleBlock(compactPanel, ".settings-center,\n  :root[data-theme-style] .settings-center");
eq(declaration(compactCenter, "grid-template-columns"), "minmax(0, 1fr)", "minimum-width settings use one content column");
eq(declaration(compactCenter, "grid-template-rows"), "auto minmax(0, 1fr)", "minimum-width navigation occupies its own top row");
const compactNav = ruleBlock(compactPanel, ".settings-center__nav,\n  :root[data-theme-style] .settings-center__nav");
eq(declaration(compactNav, "width"), "100%", "minimum-width navigation spans the settings panel");
eq(declaration(compactNav, "overflow-x"), "auto", "minimum-width navigation scrolls horizontally");
eq(declaration(compactNav, "overflow-y"), "hidden", "minimum-width navigation does not create a second vertical scroller");

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
