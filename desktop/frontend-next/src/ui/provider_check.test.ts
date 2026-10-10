import { describe, expect, it } from "vitest";
import { checkFailure } from "./provider_check";

describe("checkFailure", () => {
  it("names the host a refused redirect pointed at", () => {
    const said = checkFailure({ ok: false, code: "provider.probe.redirect_refused", target: "www.example.com" });
    expect(said).toContain("www.example.com");
    expect(said).not.toContain("{target}");
  });
});
