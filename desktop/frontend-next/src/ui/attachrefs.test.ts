import { describe, expect, it } from "vitest";
import { stripAttachmentRefs } from "./attachrefs";

describe("stripAttachmentRefs", () => {
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
