import { describe, expect, it } from "vitest";

import {
  AUTOSIZE_COMPOSER_ROWS,
  DEFAULT_COMPOSER_ROWS,
  MAX_COMPOSER_ROWS,
  getComposerTextareaSizing,
} from "../desktop/src/ui/composer-sizing";

describe("desktop composer textarea autosize (#1420)", () => {
  it("keeps the original default height below the autosize threshold", () => {
    const sizing = getComposerTextareaSizing({
      contentRows: 3,
      lineHeightPx: 20,
      verticalPaddingPx: 18,
    });

    expect(sizing.heightPx).toBe(DEFAULT_COMPOSER_ROWS * 20 + 18);
    expect(sizing.overflowY).toBe("auto");
  });

  it("starts expanding at the autosize threshold", () => {
    const sizing = getComposerTextareaSizing({
      contentRows: AUTOSIZE_COMPOSER_ROWS,
      lineHeightPx: 20,
      verticalPaddingPx: 18,
    });

    expect(sizing.heightPx).toBe(AUTOSIZE_COMPOSER_ROWS * 20 + 18);
    expect(sizing.overflowY).toBe("hidden");
  });

  it("expands between the default and maximum row counts", () => {
    const sizing = getComposerTextareaSizing({
      contentRows: 10,
      lineHeightPx: 20,
      verticalPaddingPx: 18,
    });

    expect(sizing.heightPx).toBe(10 * 20 + 18);
    expect(sizing.overflowY).toBe("hidden");
  });

  it("caps at the maximum row count and enables scrolling", () => {
    const sizing = getComposerTextareaSizing({
      contentRows: 20,
      lineHeightPx: 20,
      verticalPaddingPx: 18,
    });

    expect(sizing.heightPx).toBe(MAX_COMPOSER_ROWS * 20 + 18);
    expect(sizing.overflowY).toBe("auto");
  });
});
