import { describe, expect, it } from "vitest";
import type { User } from "./auth";
import { renderCommunity } from "./community";
import type { PackageRow } from "./registry/types";

const admin: User = {
  id: 1,
  email: "admin@example.test",
  role: "admin",
  created_at: "2026-07-22T00:00:00.000Z",
  approved_at: "2026-07-22T00:00:00.000Z",
};

const pending: PackageRow = {
  id: 42,
  kind: "plugin",
  scope_handle: "publisher",
  name: "devkit",
  slug: "publisher/devkit",
  summary: "Developer tools",
  description: "",
  source: "https://github.com/o/r",
  install_kind: "plugin",
  homepage: "",
  repo_url: "https://github.com/o/r",
  tags: "tool",
  latest_version: "2.7.1",
  install_count: 0,
  star_count: 0,
  verified: 0,
  status: "pending",
  publisher_id: 7,
  created_at: "2026-07-22T00:00:00.000Z",
  updated_at: "2026-07-22T00:30:00.000Z",
};

describe("renderCommunity", () => {
  it("binds moderation forms to the rendered package revision", () => {
    const html = renderCommunity(admin, [pending], "pending");

    expect(html).toContain('name="expectedVersion" value="2.7.1"');
    expect(html).toContain('name="expectedUpdatedAt" value="2026-07-22T00:30:00.000Z"');
    expect(html).toContain('name="expectedStatus" value="pending"');
  });
});
