import { describe, expect, it } from "vitest";
import { stripAttachmentTokens } from "./UserCard";

const path = "//path/";

describe("stripAttachmentTokens", () => {
  it("does not leave a leading empty line when a token is followed by a blank line", () => {
    expect(stripAttachmentTokens(`@${path}\n\ntext`, [path])).toBe("text");
    expect(stripAttachmentTokens(`@${path}\ntext`, [path])).toBe("text");
  });

  it("keeps the paragraph break after a token on its own line in the middle", () => {
    expect(stripAttachmentTokens(`before\n@${path}\n\nafter`, [path])).toBe("before\n\nafter");
  });

  it("keeps the rest of the message verbatim when no token is present", () => {
    expect(stripAttachmentTokens("just some words", [path])).toBe("just some words");
    expect(stripAttachmentTokens(`@${path} hello`, [path])).toBe("hello");
    expect(stripAttachmentTokens(`@${path} x`, [])).toBe(`@${path} x`);
  });
});
