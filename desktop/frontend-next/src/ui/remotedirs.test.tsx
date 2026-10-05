import { describe, expect, it } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { DirRows } from "./RemoteDirs";
import type { RemoteListing } from "../port/remote";

const at = (over: Partial<RemoteListing> = {}): RemoteListing => ({
  path: "/home/ada",
  parent: "/home",
  folders: [],
  ...over,
});

const draw = (listing: RemoteListing | null, busy = false) =>
  renderToStaticMarkup(<DirRows listing={listing} busy={busy} onGo={() => {}} />);

// The picker reads a machine that is not this one, so nothing about the rows is
// derived here: the names, and the paths behind them, are what it answered.
describe("the folders a remote machine answered with", () => {
  it("labels each row with the name that machine gave it", () => {
    const markup = draw(
      at({
        path: "/C:/Users/ada",
        folders: [{ name: "pipe line", path: "/C:/Users/ada/pipe line" }],
      }),
    );
    expect(markup).toContain("pipe line");
  });

  // A folder with nothing under it is the end of the walk, not a dead end: it
  // is the one most likely to be the project being looked for.
  it("says an empty folder can be the workspace itself", () => {
    expect(draw(at())).toContain("它本身即可作为工作区");
  });

  // Clearing the rows takes away the only account of where the reader is, and
  // "no subfolders" under a folder that has them is a lie while it loads.
  it("dims the folder it is leaving rather than emptying it", () => {
    const markup = draw(at({ folders: [{ name: "training", path: "/home/ada/training" }] }), true);
    expect(markup).toContain("data-busy");
    expect(markup).toContain("training");
  });

  it("does not claim an empty folder while it is still asking", () => {
    expect(draw(at(), true)).not.toContain("它本身即可作为工作区");
  });

  // A cap that says nothing reads as "the folder you want is not there".
  it("says so when the machine had more than it sent", () => {
    expect(draw(at({ truncated: true }))).toContain("仅列出前一部分");
    expect(draw(at())).not.toContain("仅列出前一部分");
  });

  // The first answer can be a cold dial that stops to ask for a host key. A
  // dimmed empty box says none of that, and reads as a folder with nothing in it.
  it("says it is still connecting before the first answer", () => {
    const markup = draw(null, true);
    expect(markup).toContain("正在建立连接");
    expect(markup).not.toContain("它本身即可作为工作区");
    expect(markup).not.toContain("pickrow");
  });
});
