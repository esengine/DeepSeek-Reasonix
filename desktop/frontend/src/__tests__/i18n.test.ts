import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { detectLocale, normalizeLangPref, preloadLocale } from "../lib/i18n";
import { en } from "../locales/en";

describe("ru locale", () => {
  it("detects explicit ru pref", () => {
    assert.equal(detectLocale("ru"), "ru");
  });

  it("auto-detects ru from navigator language", () => {
    const real = Object.getOwnPropertyDescriptor(globalThis, "navigator");
    try {
      Object.defineProperty(globalThis, "navigator", {
        configurable: true,
        value: { language: "ru-RU" },
      });
      assert.equal(detectLocale(""), "ru");
    } finally {
      if (real) Object.defineProperty(globalThis, "navigator", real);
      else Reflect.deleteProperty(globalThis, "navigator");
    }
  });

  it("normalizes ru pref and rejects unknown", () => {
    assert.equal(normalizeLangPref("ru"), "ru");
    assert.equal(normalizeLangPref("fr"), "");
  });

  it("preloads ru dictionary with full key parity vs en", async () => {
    await preloadLocale("ru");
    const { ru } = await import("../locales/ru");
    const missing = Object.keys(en).filter((k) => !(k in ru));
    assert.deepEqual(missing, []);
  });
});
