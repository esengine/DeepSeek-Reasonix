import { afterAll, describe, expect, it } from "vitest";
import { getLanguage, setLanguageRuntime, t } from "../src/i18n/index.js";

const originalLang = getLanguage();

afterAll(() => {
  setLanguageRuntime(originalLang);
});

describe("startup.codeRooted", () => {
  it("renders the EN banner with comma + space between rootDir and session", () => {
    setLanguageRuntime("EN");
    const out = t("startup.codeRooted", {
      rootDir: "/project",
      session: "abc",
      tools: 5,
      semantic: t("startup.semanticOn"),
    });
    expect(out).toBe(
      '\u25b8 reasonix code: rooted at /project, session "abc" \u00b7 5 native tool(s) \u00b7 semantic_search on',
    );
  });

  it("omits the semantic suffix when no semantic engine is on", () => {
    setLanguageRuntime("EN");
    const out = t("startup.codeRooted", {
      rootDir: "/project",
      session: t("startup.ephemeral"),
      tools: 0,
      semantic: "",
    });
    expect(out).toBe(
      '\u25b8 reasonix code: rooted at /project, session "(ephemeral)" \u00b7 0 native tool(s)',
    );
  });

  it("renders the zh-CN banner with the CJK comma between rootDir and session", () => {
    setLanguageRuntime("zh-CN");
    const out = t("startup.codeRooted", {
      rootDir: "/项目",
      session: "abc",
      tools: 5,
      semantic: t("startup.semanticOn"),
    });
    expect(out).toBe('▸ reasonix code：根目录 /项目，会话 "abc" · 5 个原生工具 · 语义搜索已开启');
  });
});
