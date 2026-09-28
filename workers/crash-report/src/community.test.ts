import { describe, expect, it } from "vitest";
import type { User } from "./auth";
import { renderCommunity } from "./community";
import type { ReviewRow } from "./registry/types";

const admin: User = {
  id: 1,
  email: "admin@example.test",
  role: "admin",
  created_at: "2026-07-22T00:00:00.000Z",
  approved_at: "2026-07-22T00:00:00.000Z",
};

const pending: ReviewRow = {
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
  up_count: 0,
  down_count: 0,
  rec_score: 0,
  verified: 0,
  status: "pending",
  publisher_id: 7,
  created_at: "2026-07-22T00:00:00.000Z",
  updated_at: "2026-07-22T00:30:00.000Z",
  content_hash: "",
};

describe("renderCommunity", () => {
  it("binds moderation forms to the rendered package revision", () => {
    const html = renderCommunity(admin, [pending], "pending");

    expect(html).toContain('name="expectedVersion" value="2.7.1"');
    expect(html).toContain('name="expectedUpdatedAt" value="2026-07-22T00:30:00.000Z"');
    expect(html).toContain('name="expectedStatus" value="pending"');
  });

  it("offers a live, unpinned package a digest bound to its revision", () => {
    const live = { ...pending, status: "active" };
    const html = renderCommunity(admin, [live], "active");
    const form = html.slice(html.indexOf('action="/community/publisher/devkit/pin"'));

    expect(html).toContain("unpinned");
    expect(form).toContain('name="expectedStatus" value="active"');
    expect(form).toContain('name="expectedVersion" value="2.7.1"');
    expect(form).toContain('pattern="sha256:[0-9a-f]{64}"');
    expect(html).toContain("Pin digest");
  });

  it("shows the digest a live package is pinned to and offers a re-pin", () => {
    const digest = "sha256:" + "ab".repeat(32);
    const html = renderCommunity(admin, [{ ...pending, status: "active", content_hash: digest }], "active");

    expect(html).toContain(digest);
    expect(html).toContain("Re-pin");
  });

  it("lists private packages read-only, since only their owner can submit them", () => {
    const html = renderCommunity(admin, [{ ...pending, status: "private" }], "private");
    expect(html).toContain('href="/community?status=private"');
    expect(html).toContain("Not submitted for review");
    expect(html).not.toContain("/approve");
    expect(html).not.toContain("/reject");
  });

  it("does not offer a pin before approval", () => {
    const html = renderCommunity(admin, [pending], "pending");
    expect(html).not.toContain("/pin");
  });
});
