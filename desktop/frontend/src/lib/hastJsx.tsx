// hastJsx — main-thread HAST→JSX rendering for worker-parsed markdown. Uses
// hast-util-to-jsx-runtime with the exact option set react-markdown passes
// (Fragment/jsx/jsxs from react, ignoreInvalidStyle, passKeys, passNode), so
// blocks parsed off-thread render byte-identically to react-markdown's output.
// Only lazy markdown chunks may import this module: it sits in the heavy
// vendor-markdown graph and must stay out of the app shell.

import { toJsxRuntime } from "hast-util-to-jsx-runtime";
import type { Components as JsxComponents } from "hast-util-to-jsx-runtime";
import { Fragment, jsx, jsxs } from "react/jsx-runtime";
import type { ReactNode } from "react";
import type { Components } from "react-markdown";
import type { MarkdownBlock } from "./markdownPipeline";

export function hastBlockToJsx(block: MarkdownBlock, components: Components): ReactNode {
  return toJsxRuntime(
    { type: "root", children: block.children },
    {
      Fragment,
      components: components as JsxComponents,
      ignoreInvalidStyle: true,
      jsx,
      jsxs,
      passKeys: true,
      passNode: true,
    },
  );
}
