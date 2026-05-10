import { render } from "ink-testing-library";
import React from "react";
import { afterEach, describe, expect, it } from "vitest";
import { HintRow } from "../src/cli/ui/PromptInput.js";
import { setLanguageRuntime, t } from "../src/i18n/index.js";

describe("composer hint bar — issue #564", () => {
  afterEach(() => {
    setLanguageRuntime("EN");
  });

  describe("i18n keys", () => {
    it("exposes composer.hintClear in EN", () => {
      setLanguageRuntime("EN");
      expect(t("composer.hintClear")).toBe("clear");
    });

    it("exposes composer.hintClear in zh-CN", () => {
      setLanguageRuntime("zh-CN");
      expect(t("composer.hintClear")).toBe("清空");
    });

    it("never leaks the literal key 'composer.hint' (was rendered raw before fix)", () => {
      setLanguageRuntime("EN");
      // t() falls through to returning the path when a key is missing —
      // the proposed always-visible row must be assembled from real keys.
      expect(t("composer.hintSend")).not.toBe("composer.hintSend");
      expect(t("composer.hintNewline")).not.toBe("composer.hintNewline");
      expect(t("composer.hintClear")).not.toBe("composer.hintClear");
      expect(t("composer.hintHistory")).not.toBe("composer.hintHistory");
      expect(t("composer.hintAbort")).not.toBe("composer.hintAbort");
      expect(t("composer.hintQuit")).not.toBe("composer.hintQuit");
    });
  });

  describe("HintRow rendering", () => {
    it("surfaces ^U clear on the always-visible hint row in EN", () => {
      setLanguageRuntime("EN");
      const { lastFrame, unmount } = render(<HintRow />);
      const out = lastFrame() ?? "";
      unmount();
      expect(out).toContain("^U");
      expect(out).toContain("clear");
    });

    it("renders the proposed terse hint set in EN", () => {
      setLanguageRuntime("EN");
      const { lastFrame, unmount } = render(<HintRow />);
      const out = lastFrame() ?? "";
      unmount();
      // ⏎ send · ⇧⏎ newline · ^U clear · ^P/^N history · esc abort · ^C quit
      expect(out).toContain("send");
      expect(out).toContain("newline");
      expect(out).toContain("clear");
      expect(out).toContain("^P/^N");
      expect(out).toContain("history");
      expect(out).toContain("esc");
      expect(out).toContain("abort");
      expect(out).toContain("^C");
      expect(out).toContain("quit");
    });

    it("translates verbs in zh-CN (chord glyphs stay literal)", () => {
      setLanguageRuntime("zh-CN");
      const { lastFrame, unmount } = render(<HintRow />);
      const out = lastFrame() ?? "";
      unmount();
      expect(out).toContain("^U");
      expect(out).toContain("清空");
      expect(out).toContain("发送");
      expect(out).toContain("换行");
      expect(out).toContain("历史");
      expect(out).toContain("中止");
      expect(out).toContain("退出");
    });
  });
});
