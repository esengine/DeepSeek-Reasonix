import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { StudioIcon } from "./StudioIcon";

const d = (name: "reveal" | "folder") => renderToStaticMarkup(<StudioIcon name={name} />).match(/ d="([^"]+)"/)?.[1] ?? "";

describe("the reveal-in-file-manager icon", () => {
  it("reads as an opened folder, not as a share or external-link arrow", () => {
    expect(d("reveal")).not.toBe("");
    expect(d("reveal")).not.toBe(d("folder"));
    expect(d("reveal")).not.toMatch(/l6-6/);
  });
});
