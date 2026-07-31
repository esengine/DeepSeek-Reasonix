import {
  getConfig2 as getConfig
} from "./chunk-WYO6CB5R.mjs";
import {
  __name
} from "./chunk-Y2CYZVJY.mjs";

// src/rendering-util/selectSvgElement.ts
import { select } from "d3";
var selectSvgElement = /* @__PURE__ */ __name((id) => {
  const { securityLevel } = getConfig();
  let root = select("body");
  if (securityLevel === "sandbox") {
    const sandboxElement = select(`#i${id}`);
    const doc = sandboxElement.node()?.contentDocument ?? document;
    root = select(doc.body);
  }
  const svg = root.select(`#${id}`);
  return svg;
}, "selectSvgElement");

export {
  selectSvgElement
};
