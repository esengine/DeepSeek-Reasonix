import { describe, expect, it } from "vitest";
import { docRef, outsideRef } from "./docrefs";

describe("docRef", () => {
  it("reads a reference from the document's own folder", () => {
    expect(docRef("docs/guide/DESIGN.md", "tokens.md")).toBe("docs/guide/tokens.md");
    expect(docRef("docs/guide/DESIGN.md", "./img/logo.png")).toBe("docs/guide/img/logo.png");
    expect(docRef("docs/guide/DESIGN.md", "../README.md#install")).toBe("docs/README.md");
    expect(docRef("README.md", "docs/a%20b.md?raw=1")).toBe("docs/a b.md");
  });

  it("reads a leading slash from the workspace root", () => {
    expect(docRef("docs/guide/DESIGN.md", "/src/styles/tokens.css")).toBe("src/styles/tokens.css");
  });

  it("leaves what is not a file in the workspace", () => {
    for (const href of ["https://example.com/a.md", "mailto:a@b.c", "//cdn.example/x.png", "#section", "", "../../../etc/passwd"]) {
      expect(docRef("docs/DESIGN.md", href)).toBeNull();
    }
    expect(outsideRef("javascript:alert(1)")).toBe(true);
  });
});
