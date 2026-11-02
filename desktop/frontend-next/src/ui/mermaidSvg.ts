import DOMPurify from "dompurify";
import { generate, parse, walk, type WalkOptionsNoVisit } from "css-tree";

function safeCss(css: string, context: "stylesheet" | "declarationList"): string {
  const ast = parse(css, { context });
  const options: WalkOptionsNoVisit = { leave(node, item, list) {
    if (node.type === "Atrule" || node.type === "Raw") {
      if (item) list.remove(item);
    } else if (node.type === "Declaration") {
      let unsafe = false;
      walk(node.value, (value) => {
        if (value.type === "Raw" || (value.type === "Url" && !/^#[\w-]+$/.test(value.value))) unsafe = true;
      });
      if (unsafe && item) list.remove(item);
    }
  } };
  walk(ast, options);
  return generate(ast);
}

/** Chat diagrams are inert SVG images: no HTML, navigation, animation or remote resources. */
export function sanitizeMermaidSvg(source: string): string {
  const parsed = new DOMParser().parseFromString(source, "image/svg+xml");
  if (parsed.querySelector("parsererror") || parsed.documentElement.localName !== "svg") throw new Error("Invalid SVG output");
  const clean = DOMPurify.sanitize(parsed.documentElement, {
    USE_PROFILES: { svg: true, svgFilters: true },
    FORBID_TAGS: ["foreignObject", "image", "use", "animate", "animateMotion", "animateTransform", "set", "feImage"],
    FORBID_ATTR: ["href", "xlink:href", "src", "xml:base"],
  });
  const doc = new DOMParser().parseFromString(clean, "image/svg+xml");
  if (doc.querySelector("parsererror") || doc.documentElement.localName !== "svg") throw new Error("Invalid SVG output");
  for (const style of doc.querySelectorAll("style")) {
    try { style.textContent = safeCss(style.textContent ?? "", "stylesheet"); }
    catch { style.remove(); }
  }
  for (const element of doc.querySelectorAll("*")) {
    for (const attr of [...element.attributes]) {
      if (attr.name === "style") {
        try { element.setAttribute("style", safeCss(attr.value, "declarationList")); }
        catch { element.removeAttribute("style"); }
      } else if (/url\s*\(/i.test(attr.value) && !/^url\(["']?#[\w-]+["']?\)$/.test(attr.value)) {
        element.removeAttribute(attr.name);
      }
    }
  }
  const bounds = doc.documentElement.getAttribute("viewBox")?.trim().split(/[\s,]+/).map(Number);
  if (bounds?.length === 4 && bounds.every(Number.isFinite) && bounds[2]! > 0 && bounds[3]! > 0) {
    doc.documentElement.setAttribute("width", String(bounds[2]));
    doc.documentElement.setAttribute("height", String(bounds[3]));
  }
  return new XMLSerializer().serializeToString(doc.documentElement);
}
