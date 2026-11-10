// @vitest-environment jsdom
import { describe, expect, it, vi } from "vitest";
import { HttpError } from "../port/http_error";
import type { AgentPort } from "../port/port";
import { insideWorkspace, pageEditor } from "./pageedit";

const PAGE = '<!doctype html><html><body><p id="a">hello</p></body></html>';
const target = { selector: "#a", tagName: "p" };

function portWith(over: Partial<Record<"workspaceFile" | "saveWorkspaceFile", unknown>> = {}) {
  const save = vi.fn(async (file: { content: string }) => file);
  const port = {
    workspaces: async () => ({ current: "/work/site" }),
    workspaceFile: vi.fn(async (path: string) => ({ path, content: PAGE, revision: "r1" })),
    saveWorkspaceFile: save,
    ...over,
  } as unknown as AgentPort;
  return { port, save };
}

describe("insideWorkspace", () => {
  it("names a file under the workspace by its relative path", () => {
    expect(insideWorkspace("/work/site", "/work/site/pages/a.html")).toBe("pages/a.html");
    expect(insideWorkspace("C:\\work\\site\\", "c:/Work/Site/a.html")).toBe("a.html");
  });
  it("answers nothing for a path outside, beside, or escaping the workspace", () => {
    expect(insideWorkspace("/work/site", "/work/site2/a.html")).toBe("");
    expect(insideWorkspace("/work/site", "/elsewhere/a.html")).toBe("");
    expect(insideWorkspace("/work/site", "/work/site/../x/a.html")).toBe("");
    expect(insideWorkspace("/work/site", "/work/site")).toBe("");
  });
});

describe("pageEditor", () => {
  it("writes every edit in one save, against the revision it read", async () => {
    const { port, save } = portWith();
    const out = await pageEditor(port)("/work/site/a.html", target, [
      { kind: "text", before: "hello", after: "bye" },
      { kind: "style", property: "color", value: "red" },
    ]);
    expect(out).toEqual({ saved: true, path: "a.html" });
    expect(save).toHaveBeenCalledTimes(1);
    expect(save.mock.calls[0][0]).toEqual({
      path: "a.html",
      revision: "r1",
      content: '<!doctype html><html><body><p style="color: red" id="a">bye</p></body></html>',
    });
  });

  it("saves nothing when any one edit cannot be placed", async () => {
    const { port, save } = portWith();
    const out = await pageEditor(port)("/work/site/a.html", target, [
      { kind: "style", property: "color", value: "red" },
      { kind: "text", before: "not what is there", after: "x" },
    ]);
    expect(out).toEqual({ saved: false, code: "mismatch" });
    expect(save).not.toHaveBeenCalled();
  });

  it("does not touch a file outside the workspace", async () => {
    const { port, save } = portWith();
    const out = await pageEditor(port)("/tmp/a.html", target, [{ kind: "style", property: "color", value: "red" }]);
    expect(out).toEqual({ saved: false, code: "outside" });
    expect(port.workspaceFile).not.toHaveBeenCalled();
    expect(save).not.toHaveBeenCalled();
  });

  it("tells a file that changed under the page apart from a write that failed", async () => {
    const changed = new HttpError(409, "changed", { code: "workspace.file_changed" }, true);
    const a = portWith({ saveWorkspaceFile: vi.fn().mockRejectedValue(changed) });
    expect(await pageEditor(a.port)("/work/site/a.html", target, [{ kind: "style", property: "color", value: "red" }])).toEqual({ saved: false, code: "conflict" });
    const b = portWith({ saveWorkspaceFile: vi.fn().mockRejectedValue(new Error("disk")) });
    expect(await pageEditor(b.port)("/work/site/a.html", target, [{ kind: "style", property: "color", value: "red" }])).toEqual({ saved: false, code: "failed" });
  });
});
