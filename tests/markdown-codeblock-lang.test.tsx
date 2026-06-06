import { type ReactElement, createElement } from "react";
import { describe, expect, it } from "vitest";

import { extractFencedLang as dashboardExtract } from "../dashboard/src/Markdown";
import { extractFencedLang as desktopExtract } from "../desktop/src/Markdown";

const stub = (className: string): ReactElement => createElement("code", { className });

describe.each([
  { surface: "desktop", extract: desktopExtract },
  { surface: "dashboard", extract: dashboardExtract },
])("$surface extractFencedLang", ({ extract }) => {
  it("reads language- class from a child element", () => {
    expect(extract(stub("language-ts"))).toBe("ts");
    expect(extract(stub("language-python"))).toBe("python");
    expect(extract(stub("language-c-sharp"))).toBe("c-sharp");
  });

  it("returns 'text' when no language- class is found", () => {
    expect(extract(stub(""))).toBe("text");
    expect(extract(stub("not-a-lang"))).toBe("text");
    expect(extract("just a string")).toBe("text");
    expect(extract(undefined)).toBe("text");
  });

  it("ignores non-string className values", () => {
    expect(extract(createElement("code", { className: 42 }))).toBe("text");
  });
});
