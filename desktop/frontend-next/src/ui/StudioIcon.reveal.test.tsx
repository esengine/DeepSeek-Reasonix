import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { StudioIcon } from "./StudioIcon";

describe("the reveal-in-file-manager icon", () => {
  it("reads as an opened folder, not as a share or external-link arrow", () => {
    const icon = renderToStaticMarkup(<StudioIcon name="reveal" />);
    expect(icon).toContain("lucide-folder-open");
    expect(icon).not.toContain("lucide-external-link");
  });
});
