// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render } from "@testing-library/react";
import "./testkit";
import { ProductionManifest, isProductionManifest, parseManifest } from "./ProductionManifest";

afterEach(cleanup);

const manifest = JSON.stringify({
  version: 1, production_id: "teaser", revision: 7, brief: { revision: 2 },
  shots: [{ shot_id: "s01", start: 0, end: 5, prompt_revision: 1 }, { shot_id: "s02", start: 5, end: 10, prompt_revision: 2 }],
  artifacts: [
    { artifact_id: "s01", version: 1, shot_id: "s01", path: "video/teaser/s01-v1.mp4", sha256: "aa" },
    { artifact_id: "s02", version: 1, shot_id: "s02", path: "video/teaser/s02-v1.mp4", sha256: "bb", superseded_by: 2 },
    { artifact_id: "s02", version: 2, shot_id: "s02", path: "video/teaser/s02-v2.mp4", sha256: "cc" },
  ],
  reviews: [
    { artifact_id: "s01", version: 1, sha256: "aa", brief_revision: 2, scope: "technical", verdict: "pass" },
    { artifact_id: "s01", version: 1, sha256: "aa", brief_revision: 1, scope: "content", verdict: "pass" },
    { artifact_id: "s02", version: 2, sha256: "cc", brief_revision: 2, scope: "audio_and_sync", verdict: "unverified" },
  ],
  delivery: null, x_future: { keep: true },
});

describe("ProductionManifest", () => {
  it("matches only the manifest path the skill writes", () => {
    expect(isProductionManifest("video/teaser/manifest.json")).toBe(true);
    expect(isProductionManifest("sub/video/teaser/manifest.json")).toBe(true);
    expect(isProductionManifest("video/manifest.json")).toBe(false);
    expect(isProductionManifest("video/teaser/notes/manifest.json")).toBe(false);
  });

  it("keeps superseded versions and marks a review stale when its brief revision moved", () => {
    const parsed = parseManifest(manifest);
    if ("unsupported" in parsed) throw new Error(parsed.unsupported);
    const [s01, s02] = parsed.rows;
    expect(s02.versions.map((v) => [v.version, v.current])).toEqual([[1, false], [2, true]]);
    expect(s01.versions[0].reviews).toEqual([
      { scope: "technical", verdict: "pass", stale: false },
      { scope: "content", verdict: "pass", stale: true },
    ]);
  });

  it("refuses a manifest version it does not know instead of guessing", () => {
    const { getByRole } = render(<ProductionManifest text={JSON.stringify({ version: 2 })} onOpen={() => {}} />);
    expect(getByRole("alert").textContent).toContain("2");
  });

  it("opens a version's file from its row", () => {
    const onOpen = vi.fn();
    const { getAllByRole } = render(<ProductionManifest text={manifest} onOpen={onOpen} />);
    fireEvent.click(getAllByRole("button", { name: "v2" })[0]);
    expect(onOpen).toHaveBeenCalledWith("video/teaser/s02-v2.mp4");
  });
});
