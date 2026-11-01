import { describe, expect, it } from "vitest";
import { stripAttachmentRefs } from "./attachrefs";

describe("stripAttachmentRefs", () => {
  it("hides an external-folder token once an image backs it", () => {
    const text = "@__reasonix_external_folder/ab12cd/outside.png 请看";
    const out = stripAttachmentRefs(text, [
      { path: "c:/folder/outside.png", ref: "__reasonix_external_folder/ab12cd/outside.png" },
    ]);
    expect(out).toBe("请看");
  });

  it("leaves a text-file external token that names no image", () => {
    const text = "@__reasonix_external_folder/ab12cd/notes.txt 请看";
    const out = stripAttachmentRefs(text, [
      { path: "c:/folder/outside.png", ref: "__reasonix_external_folder/ab12cd/outside.png" },
    ]);
    expect(out).toBe(text);
  });

  it("strips a workspace attachment token", () => {
    const text = "@.reasonix/attachments/x.png 请看";
    const out = stripAttachmentRefs(text, [{ path: ".reasonix/attachments/x.png" }]);
    expect(out).toBe("请看");
  });

  it("keeps a workspace token whose image failed to load", () => {
    const text = "@.reasonix/attachments/x.png 请看";
    const out = stripAttachmentRefs(text, [{ path: ".reasonix/attachments/x.png" }], new Set([".reasonix/attachments/x.png"]));
    expect(out).toBe(text);
  });

  it("keeps a code block's indentation when a workspace token is stripped", () => {
    const text = "@.reasonix/attachments/x.png\n    def foo():\n        return 1";
    const out = stripAttachmentRefs(text, [{ path: ".reasonix/attachments/x.png" }]);
    expect(out).toBe("\n    def foo():\n        return 1");
  });

  it("keeps a code block's indentation when an external token is stripped", () => {
    const text = "@__reasonix_external_folder/ab12cd/outside.png\n    def foo():\n        return 1";
    const out = stripAttachmentRefs(text, [
      { path: "c:/folder/outside.png", ref: "__reasonix_external_folder/ab12cd/outside.png" },
    ]);
    expect(out).toBe("\n    def foo():\n        return 1");
  });

  it("drops at most one adjacent space and collapses only the local gap", () => {
    const text = "word1 @.reasonix/attachments/x.png word2";
    const out = stripAttachmentRefs(text, [{ path: ".reasonix/attachments/x.png" }]);
    expect(out).toBe("word1 word2");
  });

  it("does not compact whitespace elsewhere in the message", () => {
    const text = "@.reasonix/attachments/x.png see:\n\n    line one\n    line two";
    const out = stripAttachmentRefs(text, [{ path: ".reasonix/attachments/x.png" }]);
    expect(out).toBe("see:\n\n    line one\n    line two");
  });
});
