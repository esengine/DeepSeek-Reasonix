import { describe, expect, it } from "vitest";
import type { PackageRow } from "./types";
import { toPackageDTO } from "./types";

const row: PackageRow = {
  id: 1,
  kind: "skill",
  scope_handle: "publisher",
  name: "demo",
  slug: "publisher/demo",
  summary: "",
  description: "",
  source: "https://github.com/o/r/tree/main/skills/demo",
  install_kind: "auto",
  homepage: "",
  repo_url: "https://github.com/o/r",
  tags: "tool,coding",
  latest_version: "1.0.0",
  install_count: 0,
  star_count: 0,
  verified: 0,
  status: "active",
  publisher_id: 1,
  created_at: "2026-07-22T00:00:00.000Z",
  updated_at: "2026-07-22T00:00:00.000Z",
};

describe("toPackageDTO", () => {
  it("normalizes legacy auto and mismatched rows to the declared kind", () => {
    expect(toPackageDTO(row).installKind).toBe("skill");
    expect(toPackageDTO({ ...row, install_kind: "mcp" }).installKind).toBe("skill");
    expect(toPackageDTO({ ...row, kind: "mcp", install_kind: "mcp" }).installKind).toBe("mcp");
    expect(toPackageDTO({ ...row, kind: "plugin", install_kind: "plugin" }).installKind).toBe("plugin");
  });
});
