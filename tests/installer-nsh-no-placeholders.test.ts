/** Regression for #928 — Tauri's NSIS template stores `{{product_name}}` and substitutes it at installer runtime via the nsis_tauri_utils DLL. When that DLL fails to load (AV / temp-dir issues) the raw placeholder leaks into the dialog. Our custom language files must hardcode the product name so the strings degrade gracefully. */

import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";

const nshFiles = [
  "../desktop/src-tauri/nsis/English.nsh",
  "../desktop/src-tauri/nsis/SimpChinese.nsh",
];

const PRODUCT_NAME = "Reasonix";
const RUNTIME_STRINGS = ["appRunning", "appRunningOkKill", "failedToKillApp"] as const;

describe("desktop installer NSIS language files — issue #928", () => {
  for (const rel of nshFiles) {
    describe(rel.split("/").pop() ?? rel, () => {
      const text = readFileSync(fileURLToPath(new URL(rel, import.meta.url)), "utf8");
      const code = text
        .split(/\r?\n/)
        .filter((line) => !line.trimStart().startsWith(";"))
        .join("\n");

      it("contains no `{{product_name}}` runtime placeholders", () => {
        expect(code).not.toMatch(/\{\{\s*product_name\s*\}\}/);
      });

      for (const stringName of RUNTIME_STRINGS) {
        it(`mentions the product name in LangString ${stringName}`, () => {
          const re = new RegExp(
            `LangString\\s+${stringName}\\s+\\$\\{LANG_[A-Z_]+\\}\\s+"[^"]*${PRODUCT_NAME}[^"]*"`,
          );
          expect(text).toMatch(re);
        });
      }
    });
  }
});
