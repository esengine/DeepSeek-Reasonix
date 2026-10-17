import assert from "node:assert/strict";
import { ordinaryFileSource } from "../lib/attachDedup";
import { formatWorkspaceReference, parseWorkspaceReference } from "../lib/workspaceDrag";
import { isExternalWorkspaceReference, restoreExternalFolderReferences } from "../lib/attachmentSubmit";
import type { AppBindings } from "../lib/bridge";

const a = new File(["first"], "app.apk", { lastModified: 1 });
const b = new File(["other"], "app.apk", { lastModified: 1 });
assert.equal(ordinaryFileSource(a, ""), ordinaryFileSource(a, ""));
assert.notEqual(ordinaryFileSource(a, ""), ordinaryFileSource(b, ""));
assert.equal(ordinaryFileSource(a, "/outside/app.apk"), "path:/outside/app.apk");
for (const path of ["/outside/large app.apk", "C:\\Downloads\\large app.apk", "src/main.go"]) {
  assert.equal(parseWorkspaceReference(formatWorkspaceReference(path))?.path, path);
}
assert.equal(isExternalWorkspaceReference({ path: "/outside/large app.apk", displayPath: "/outside/large app.apk" }), true);
assert.equal(isExternalWorkspaceReference({ path: "C:/Downloads/large app.apk", displayPath: "C:/Downloads/large app.apk" }), true);
assert.equal(isExternalWorkspaceReference({ path: "src/main.go" }), false);
const calls: string[] = [];
const bindings: Partial<AppBindings> = {
  CaptureAttachmentTarget: async () => ({ token: "captured", capabilities: ["attachments-v2"] }),
  AttachDroppedForTarget: async (token: string, path: string) => { calls.push(`${token}:${path}`); return { kind: "workspace", path, displayPath: path }; },
  ReleaseAttachmentTarget: async (token: string) => { calls.push(`release:${token}`); },
};
await restoreExternalFolderReferences(bindings as AppBindings, { kind: "session", tabId: "source" }, [{ path: "/outside/large app.apk", displayPath: "/outside/large app.apk" }]);
assert.deepEqual(calls, ["captured:/outside/large app.apk", "release:captured"]);
console.log("attachment references: object identity, escaped paths and restored exact-file authorization passed");
