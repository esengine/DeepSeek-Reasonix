import {
  getConfig2 as getConfig
} from "./chunk-CHAKFXHA.mjs";
import {
  select_default
} from "./chunk-FO5PYUIK.mjs";
import {
  __name
} from "./chunk-PTVI3W5X.mjs";

// src/rendering-util/selectSvgElement.ts
var selectSvgElement = /* @__PURE__ */ __name((id) => {
  const { securityLevel } = getConfig();
  let root = select_default("body");
  if (securityLevel === "sandbox") {
    const sandboxElement = select_default(`#i${id}`);
    const doc = sandboxElement.node()?.contentDocument ?? document;
    root = select_default(doc.body);
  }
  const svg = root.select(`#${id}`);
  return svg;
}, "selectSvgElement");

export {
  selectSvgElement
};
