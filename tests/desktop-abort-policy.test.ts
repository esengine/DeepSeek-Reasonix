import { describe, expect, it } from "vitest";
import { desktopUserAbortLoopOptions } from "../src/cli/commands/desktop.js";

describe("desktop explicit abort policy", () => {
  it("keeps the interrupted user turn in model context", () => {
    expect(desktopUserAbortLoopOptions()).toBeUndefined();
  });
});
