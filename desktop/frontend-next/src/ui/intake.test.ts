import { describe, expect, it } from "vitest";
import { actionFor, countLines, pasteIsLong, planIntake, planTone, planVerb, relativize } from "./intake";

const ROOT = "D:/DeepSeek-Reasonix";
const file = (path: string, mime = "") => ({ kind: "file" as const, path, mime });
const dir = (path: string) => ({ kind: "dir" as const, path });
const text = (body: string) => ({ kind: "text" as const, text: body });

describe("what a dropped thing turns into", () => {
  it("carries pixels and points at everything else", () => {
    expect(actionFor(file(`${ROOT}/shot.png`, "image/png"), ROOT)).toBe("attach");
    expect(actionFor(file(`${ROOT}/internal/hook/runner.go`), ROOT)).toBe("ref");
    expect(actionFor(dir(`${ROOT}/internal/hook`), ROOT)).toBe("ref");
    expect(actionFor(text("hello"), ROOT)).toBe("insert");
  });

  // A reference that does not resolve is worse than refusing the drop: the
  // agent reads it, finds nothing, and the turn is spent on the lookup.
  it("refuses a file it could not reference and has no bytes for", () => {
    expect(actionFor(file("C:/elsewhere/notes.md"), ROOT)).toBe("reject");
    expect(actionFor(file(`${ROOT}/a.go`), undefined)).toBe("reject");
  });

  // A browser tab is never told a path. Bytes in hand are still worth carrying,
  // which is the only answer available there.
  it("carries bytes when no path was reported", () => {
    const dropped = { kind: "file" as const, name: "notes.md", blob: new Blob(["x"]) };
    expect(actionFor(dropped, ROOT)).toBe("attach");
  });

  // A dragover reports kinds and never payloads, so the preview has to commit
  // before it can resolve anything — otherwise every drag previews as refused.
  it("previews a file still in the air as a reference", () => {
    expect(actionFor({ kind: "file", mime: "", pending: true }, ROOT)).toBe("ref");
    expect(actionFor({ kind: "file", mime: "image/png", pending: true }, ROOT)).toBe("attach");
  });

  // An image is carried wherever it came from — bytes travel, paths do not.
  it("still attaches an image from outside the workspace", () => {
    expect(actionFor(file("C:/elsewhere/shot.png", "image/png"), ROOT)).toBe("attach");
  });
});

describe("relativize", () => {
  it("takes the workspace-relative form an @ref uses", () => {
    expect(relativize(`${ROOT}/internal/hook/runner.go`, ROOT)).toBe("internal/hook/runner.go");
    expect(relativize(`${ROOT}\\internal\\hook`, ROOT)).toBe("internal/hook");
    expect(relativize(ROOT, ROOT)).toBe(".");
  });

  it("does not mistake a sibling directory for a child", () => {
    expect(relativize("D:/DeepSeek-Reasonix-old/x.go", ROOT)).toBeNull();
  });

  // Windows hands back whatever case the user typed; the same file must not
  // become unreferenceable because the drive letter arrived lowercased.
  it("matches the root case-insensitively", () => {
    expect(relativize(`d:/deepseek-reasonix/main.go`, ROOT)).toBe("main.go");
  });
});

describe("the plan and the sentence it says", () => {
  it("splits a mixed drop and says both halves", () => {
    const plan = planIntake(
      [file(`${ROOT}/a.png`, "image/png"), file(`${ROOT}/b.go`), file(`${ROOT}/c.go`)],
      ROOT,
    );
    expect(plan.attach).toHaveLength(1);
    expect(plan.ref).toEqual(["b.go", "c.go"]);
    expect(planTone(plan)).toBe("attach");
    expect(planVerb(plan)).toContain("1");
    expect(planVerb(plan)).toContain("2");
  });

  it("names the one file it is about to reference", () => {
    const plan = planIntake([file(`${ROOT}/internal/hook/runner.go`)], ROOT);
    expect(planVerb(plan)).toContain("runner.go");
  });

  it("marks a directory so the reference reads as one", () => {
    expect(planIntake([dir(`${ROOT}/internal/hook`)], ROOT).ref).toEqual(["internal/hook/"]);
  });

  // The unit was right and the composition threw the answer away: actionFor
  // said "ref", then the plan asked for a path the drag does not have yet and
  // dropped it. An empty plan shows no hint, so a source file dragged over the
  // pane looked exactly like a dead feature.
  it("counts a reference it cannot name yet instead of losing it", () => {
    const plan = planIntake([{ kind: "file", mime: "", pending: true }], ROOT);
    expect(plan.pendingRef).toBe(1);
    expect(planTone(plan)).toBe("ref");
    expect(planVerb(plan)).not.toBe("");
  });

  // The desktop channel reports a path and never the bytes. Filtering the plan
  // by blob alone left the drop with nothing to do and no way to say so.
  it("keeps a host-named image in the plan though it carries no bytes", () => {
    const plan = planIntake([file(`${ROOT}/shot.png`, "image/png")], ROOT);
    expect(plan.attach).toHaveLength(1);
    expect(plan.attach[0].blob).toBeUndefined();
    expect(plan.attach[0].path).toContain("shot.png");
  });

  it("says why nothing will happen rather than going quiet", () => {
    const plan = planIntake([file("C:/elsewhere/notes.md")], ROOT);
    expect(planTone(plan)).toBe("reject");
    expect(planVerb(plan)).not.toBe("");
  });

  // The regression this whole change exists for: dragging a selection used to
  // reach a handler that prevented the default and then did nothing.
  it("keeps dropped text", () => {
    const plan = planIntake([text("func main() {}")], ROOT);
    expect(plan.insert).toBe("func main() {}");
    expect(planTone(plan)).toBe("insert");
  });
});

describe("a paste long enough to bury the composer", () => {
  it("counts lines and characters, whichever comes first", () => {
    expect(pasteIsLong("short")).toBe(false);
    expect(pasteIsLong("x".repeat(4001))).toBe(true);
    // 800 narrow lines are well under the character cap and have still eaten
    // the window, which is the case a character-only threshold misses.
    expect(pasteIsLong(Array.from({ length: 800 }, () => "ok").join("\n"))).toBe(true);
  });

  it("counts an empty paste as no lines", () => {
    expect(countLines("")).toBe(0);
    expect(countLines("a\nb")).toBe(2);
  });
});
