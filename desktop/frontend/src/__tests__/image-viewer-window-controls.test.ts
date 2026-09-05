// Run: tsx src/__tests__/image-viewer-window-controls.test.ts

import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

const styles = readFileSync(new URL("../styles.css", import.meta.url), "utf8").replace(/\/\*[\s\S]*?\*\//g, "");

function finalDeclaration(selector: string, property: string): string | undefined {
  let value: string | undefined;
  const rule = /([^{}]+)\{([^{}]*)\}/g;
  let ruleMatch: RegExpExecArray | null;

  while ((ruleMatch = rule.exec(styles)) !== null) {
    const selectors = ruleMatch[1].split(",").map((part) => part.trim());
    if (!selectors.includes(selector)) continue;

    const declaration = new RegExp(`(?:^|;)\\s*${property}\\s*:\\s*([^;]+)`, "g");
    let declarationMatch: RegExpExecArray | null;
    while ((declarationMatch = declaration.exec(ruleMatch[2])) !== null) {
      value = declarationMatch[1].trim();
    }
  }

  return value;
}

const windowsPreviewSelector = ".app--windows-frameless:has(.image-viewer-backdrop) > .windows-window-controls";
const windowsAutomationSelector = ".app--windows-frameless.app--automation > .windows-window-controls";

assert.equal(finalDeclaration(".image-viewer__close", "right"), "16px");
assert.equal(finalDeclaration(".app--windows-frameless .image-viewer__close", "right"), undefined);
assert.equal(finalDeclaration(windowsPreviewSelector, "opacity"), "0");
assert.equal(finalDeclaration(windowsPreviewSelector, "visibility"), "hidden");
assert.equal(finalDeclaration(windowsPreviewSelector, "pointer-events"), "none");

// The entire automation page (list, editor, and loading fallback) owns this
// region. Hiding only the icon would leave invisible caption click targets.
assert.equal(finalDeclaration(windowsAutomationSelector, "opacity"), "0");
assert.equal(finalDeclaration(windowsAutomationSelector, "visibility"), "hidden");
assert.equal(finalDeclaration(windowsAutomationSelector, "pointer-events"), "none");

console.log("image viewer and automation window controls: 8 passed");
