import { describe, expect, it } from "vitest";
import { detectEditor, normalizeEditorBuffer } from "../src/cli/edit/external-editor.js";

describe("detectEditor (issue #647)", () => {
  it("returns null when no editor env var is set", () => {
    expect(detectEditor({})).toBeNull();
  });

  it("GIT_EDITOR wins over VISUAL and EDITOR", () => {
    expect(
      detectEditor({
        GIT_EDITOR: "git-pref",
        VISUAL: "visual-pref",
        EDITOR: "editor-pref",
      }),
    ).toBe("git-pref");
  });

  it("VISUAL wins over EDITOR when GIT_EDITOR is unset", () => {
    expect(detectEditor({ VISUAL: "visual-pref", EDITOR: "editor-pref" })).toBe("visual-pref");
  });

  it("falls back to EDITOR last", () => {
    expect(detectEditor({ EDITOR: "nano" })).toBe("nano");
  });

  it("trims surrounding whitespace", () => {
    expect(detectEditor({ EDITOR: "  nano --noplugin  " })).toBe("nano --noplugin");
  });

  it("treats an empty / whitespace-only var as unset", () => {
    expect(detectEditor({ EDITOR: "   " })).toBeNull();
    expect(detectEditor({ EDITOR: "" })).toBeNull();
  });
});

describe("normalizeEditorBuffer", () => {
  it("normalizes Windows CRLF and bare CR while preserving the existing one-trailing-newline strip", () => {
    expect(normalizeEditorBuffer("first\r\nsecond\rthird\r\n")).toBe("first\nsecond\nthird");
  });
});
