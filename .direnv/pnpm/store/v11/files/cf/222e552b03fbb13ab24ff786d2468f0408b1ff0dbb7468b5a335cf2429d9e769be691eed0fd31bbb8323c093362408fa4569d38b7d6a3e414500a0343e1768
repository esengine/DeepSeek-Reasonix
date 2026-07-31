import {
  __name
} from "./chunk-PTVI3W5X.mjs";

// src/rendering-util/layout-algorithms/ddlt/captureContract.ts
var DDLT_SIZE_CAPTURE_VERSION = 1;

// src/rendering-util/layout-algorithms/ddlt/sizeCapture.ts
function getCaptureGlobal() {
  if (typeof globalThis === "undefined") {
    return void 0;
  }
  return globalThis;
}
__name(getCaptureGlobal, "getCaptureGlobal");
function shouldCaptureSizes() {
  return Boolean(getCaptureGlobal()?.mermaidCaptureSizes);
}
__name(shouldCaptureSizes, "shouldCaptureSizes");
function capturedFromLocation() {
  if (typeof location === "undefined") {
    return "browser-dev";
  }
  return `${location.pathname}${location.search}`;
}
__name(capturedFromLocation, "capturedFromLocation");
function emitCapturedSizes(captured, element) {
  const g = getCaptureGlobal();
  if (!g) {
    return;
  }
  const domNode = element.node();
  const ownerSvg = (domNode && "ownerSVGElement" in domNode ? domNode.ownerSVGElement : null) ?? domNode;
  const svgId = ownerSvg?.id ?? "(unknown)";
  g.mermaidCapturedSizes ??= [];
  const entry = { svgId, sizes: captured };
  g.mermaidCapturedSizes.push(entry);
  g.mermaidLastCapturedSizes = entry;
}
__name(emitCapturedSizes, "emitCapturedSizes");
function captureNodeSizes(element, data4Layout) {
  const nodes = [];
  for (const node of data4Layout.nodes) {
    if (node.isGroup) {
      continue;
    }
    nodes.push({ id: node.id, width: node.width ?? 0, height: node.height ?? 0 });
  }
  if (nodes.length === 0) {
    return;
  }
  emitCapturedSizes(
    {
      metadata: {
        captureVersion: DDLT_SIZE_CAPTURE_VERSION,
        capturedAt: (/* @__PURE__ */ new Date()).toISOString(),
        capturedFrom: capturedFromLocation()
      },
      nodes
    },
    element
  );
}
__name(captureNodeSizes, "captureNodeSizes");
export {
  captureNodeSizes,
  shouldCaptureSizes
};
