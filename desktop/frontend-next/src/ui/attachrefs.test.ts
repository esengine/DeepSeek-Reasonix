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
});
