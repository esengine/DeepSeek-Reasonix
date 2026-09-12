import { act } from "react";
import { flushPromises, renderFilesWorkspace, waitFor } from "./workspace-panel-test-harness";

const pageCalls: Array<{ offset: number; version: string }> = [];
const { dom, root } = await renderFilesWorkspace({
  ReadPresentedFileForTab: async (_tabId, _toolCallId, path) => ({
    path, body: "first\n", size: 18, truncated: true, binary: false, version: "18:1", nextOffset: 6,
  }),
  ReadPresentedFileSourceForTab: async (_tabId, _toolCallId, path) => ({
    path, body: "first\n", size: 18, truncated: true, binary: false, version: "18:1", nextOffset: 6,
  }),
  ReadPresentedTextPageForTab: async (_tabId, _toolCallId, path, offset, version) => {
    pageCalls.push({ offset, version });
    return { path, body: "second\nthird\n", offset, nextOffset: 18, size: 18, hasMore: false, version };
  },
}, {
  revealPathRequest: { id: 1, path: "/tmp/large.txt", toolCallId: "present-1", action: "preview" },
});

await waitFor("presented preview", () => document.body.textContent?.includes("first") === true);
if (!/external files|外部文件|外部檔案/i.test(document.body.textContent ?? "")) {
  throw new Error("an absolute presented path was not identified as an external-file scope");
}
const scopeFilter = document.querySelector<HTMLInputElement>(".workspace-search input");
if (!scopeFilter || !/external files|外部文件|外部檔案/i.test(scopeFilter.placeholder)) {
  throw new Error("the external-file scope did not expose a matching filter label");
}
const loadMore = Array.from(document.querySelectorAll<HTMLButtonElement>("button"))
  .find(button => /load more|加载更多|載入更多/i.test(button.textContent ?? ""));
if (!loadMore) throw new Error("presented text preview did not expose Load more");
await act(async () => {
  loadMore.dispatchEvent(new window.MouseEvent("click", { bubbles: true }));
  await flushPromises();
});
await waitFor("appended text page", () => document.body.textContent?.includes("third") === true);
if (pageCalls.length !== 1 || pageCalls[0]?.offset !== 6 || pageCalls[0]?.version !== "18:1") {
  throw new Error(`pagination did not preserve offset/version: ${JSON.stringify(pageCalls)}`);
}
if (!/full file loaded|已加载全文|已載入全文/i.test(document.body.textContent ?? "")) {
  throw new Error("completed pagination did not report the full file state");
}

await act(async () => root.unmount());
dom.window.close();
console.log("workspace presented text pagination passed");
