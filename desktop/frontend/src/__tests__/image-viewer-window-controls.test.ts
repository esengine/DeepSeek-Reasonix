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

assert.equal(finalDeclaration(".image-viewer__close", "right"), "16px");
assert.equal(finalDeclaration(".app--windows-frameless .image-viewer__close", "right"), undefined);
assert.equal(finalDeclaration(windowsPreviewSelector, "opacity"), "0");
assert.equal(finalDeclaration(windowsPreviewSelector, "visibility"), "hidden");
assert.equal(finalDeclaration(windowsPreviewSelector, "pointer-events"), "none");

console.log("image viewer window controls: 5 passed");
