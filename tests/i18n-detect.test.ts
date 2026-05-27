import { afterEach, describe, expect, it } from "vitest";
import { detectSystemLanguage } from "../src/i18n/index.js";

describe("detectSystemLanguage", () => {
  const originalEnv = {
    LANG: process.env.LANG,
    LC_ALL: process.env.LC_ALL,
    LC_MESSAGES: process.env.LC_MESSAGES,
  };

  afterEach(() => {
    process.env.LANG = originalEnv.LANG;
    process.env.LC_ALL = originalEnv.LC_ALL;
    process.env.LC_MESSAGES = originalEnv.LC_MESSAGES;
  });

  it("maps zh-CN to zh-CN", () => {
    expect(detectSystemLanguage("zh-CN")).toBe("zh-CN");
  });

  it("maps any zh-* variant to zh-CN", () => {
    expect(detectSystemLanguage("zh-TW")).toBe("zh-CN");
    expect(detectSystemLanguage("zh-HK")).toBe("zh-CN");
    expect(detectSystemLanguage("zh")).toBe("zh-CN");
  });

  it("maps en-* variants to EN", () => {
    expect(detectSystemLanguage("en-US")).toBe("EN");
    expect(detectSystemLanguage("en-GB")).toBe("EN");
    expect(detectSystemLanguage("en")).toBe("EN");
  });

  it("maps de-* variants to de", () => {
    expect(detectSystemLanguage("de-DE")).toBe("de");
    expect(detectSystemLanguage("de-AT")).toBe("de");
    expect(detectSystemLanguage("de")).toBe("de");
  });

  it("maps ja-* variants to ja", () => {
    expect(detectSystemLanguage("ja-JP")).toBe("ja");
    expect(detectSystemLanguage("ja")).toBe("ja");
  });

  it("returns null for unsupported locales", () => {
    expect(detectSystemLanguage("fr-FR")).toBeNull();
    expect(detectSystemLanguage("")).toBeNull();
  });

  it("default arg reads from Intl — returns a valid LanguageCode or null", () => {
    const result = detectSystemLanguage();
    expect(
      result === null ||
        result === "EN" ||
        result === "zh-CN" ||
        result === "de" ||
        result === "ru" ||
        result === "ja",
    ).toBe(true);
  });

  it("default arg reads de from locale environment variables", () => {
    process.env.LANG = "de_DE.UTF-8";
    process.env.LC_ALL = "";
    process.env.LC_MESSAGES = "";
    expect(detectSystemLanguage()).toBe("de");
  });
});
