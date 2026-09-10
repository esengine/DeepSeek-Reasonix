import assert from "node:assert/strict";
import { test } from "node:test";
import { DialogHost, fileFiltersFromPatterns, type DialogApi } from "./dialogs.js";

test("Wails filter patterns become Electron extension lists", () => {
  assert.deepEqual(fileFiltersFromPatterns([
    { displayName: "Images", pattern: "*.png;*.jpg; *.JPEG" },
    { displayName: "", pattern: "*.tar.gz" },
    { displayName: "All", pattern: "*.*" },
    { pattern: "*" },
    { displayName: "broken" },
    "junk",
  ]), [
    { name: "Images", extensions: ["png", "jpg", "JPEG"] },
    { name: "*.tar.gz", extensions: ["tar.gz"] },
    { name: "All", extensions: ["*"] },
    { name: "*", extensions: ["*"] },
  ]);
  assert.deepEqual(fileFiltersFromPatterns(undefined), []);
});

test("message boxes map buttons by name and return the chosen label", async () => {
  const seen: unknown[] = [];
  const dialog = {
    showOpenDialog: async () => ({ canceled: true, filePaths: [] }),
    showSaveDialog: async () => ({ canceled: true }),
    showMessageBox: async (...args: unknown[]) => {
      seen.push(args[args.length - 1]);
      return { response: 1, checkboxChecked: false };
    },
  } as unknown as DialogApi;
  const host = new DialogHost(dialog, () => undefined);
  const result = await host.message({ type: "question", title: "T", message: "M", buttons: ["Keep", "Discard"], defaultButton: "Keep", cancelButton: "Discard" });
  assert.deepEqual(result, { button: "Discard" });
  assert.deepEqual(seen[0], { type: "question", title: "T", message: "M", buttons: ["Keep", "Discard"], defaultId: 0, cancelId: 1, noLink: true });
  assert.deepEqual(await host.openDirectory({ title: "Pick" }), { path: "" });
  assert.deepEqual(await host.openFile({ multiple: true }), { paths: [] });
  assert.deepEqual(await host.saveFile({ defaultDirectory: "/tmp", defaultFilename: "a.txt" }), { path: "" });
});
