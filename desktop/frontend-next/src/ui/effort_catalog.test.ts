import { describe, expect, it, vi } from "vitest";
import { boot, current } from "../i18n";
import { effortDescription, effortLabel, effortMenu } from "./effort";

// The provider contract owns the GLM ladder; presentation must cover its values.
const SOURCES = import.meta.glob("../../../../internal/contract/provider/zhipu_effort.go", { query: "?raw", import: "default", eager: true }) as Record<string, string>;
const source = Object.values(SOURCES)[0] ?? "";
const ladders = [...source.matchAll(/\bLevels:\s*\[\]string\{([^}]+)\}/g)].map((match) => [...match[1].matchAll(/"([^"]+)"/g)].map((value) => value[1]));
const levels = [...new Set(ladders.flat())];

describe("GLM catalog effort presentation", () => {
  for (const lang of ["zh", "en"] as const) {
    it(`labels and describes every catalog level in ${lang}`, () => {
      const previous = current();
      vi.stubGlobal("localStorage", { getItem: () => lang });
      vi.stubGlobal("document", { documentElement: { lang: "" } });
      boot();
      try {
        expect(ladders.length).toBeGreaterThan(0);
        expect(levels).toContain("minimal");
        for (const level of levels) {
          expect(effortLabel(level), level).not.toBe(level);
          expect(effortDescription(level), level).not.toBe(level);
          if (lang === "en") {
            expect(effortLabel(level), level).not.toMatch(/[一-鿿]/);
            expect(effortDescription(level), level).not.toMatch(/[一-鿿]/);
          }
        }
        expect(effortLabel("minimal")).toBe(lang === "zh" ? "最轻量" : "Minimal");
        expect(effortLabel("minimal")).not.toBe(effortLabel("none"));
        expect(effortLabel("minimal")).not.toBe(effortLabel("low"));
        expect(effortDescription("minimal")).toContain(lang === "zh" ? "GLM-5.2 会跳过思考" : "GLM-5.2 skips thinking");
        const rows = effortMenu(["auto", ...levels], "glm-5.2", "__declare");
        expect(rows.find((row) => row.value === "minimal")?.desc).toBe(effortDescription("minimal"));
      } finally {
        vi.stubGlobal("localStorage", { getItem: () => previous });
        boot();
        vi.unstubAllGlobals();
      }
    });
  }
});
