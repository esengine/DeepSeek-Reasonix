// markdown.worker — off-main-thread Markdown parse (Phase E). Receives the
// raw answer text, runs the isomorphic pipeline (normalizeMath → remark →
// rehype → react-markdown transforms → block slicing), and posts back
// JSON-serializable HAST blocks. The worker never cancels in-flight work; the
// client drops stale responses by request id.

import { parseMarkdown } from "../lib/markdownPipeline";
import type { MarkdownParseRequest, MarkdownParseResponse } from "../lib/markdownWorkerClient";

const workerScope = globalThis as unknown as {
  onmessage: ((event: MessageEvent<MarkdownParseRequest>) => void) | null;
  postMessage: (response: MarkdownParseResponse) => void;
};

workerScope.onmessage = (event) => {
  const { id, text } = event.data;
  try {
    workerScope.postMessage({ id, result: parseMarkdown(text) });
  } catch (error) {
    workerScope.postMessage({
      id,
      error: error instanceof Error ? error.message : String(error),
    });
  }
};
