import rehypeKatex from "rehype-katex";

type HastNode = {
  type: string;
  tagName?: string;
  value?: string;
  properties?: Record<string, unknown>;
  children?: HastNode[];
};

const SOURCE_MARKER = "reasonix-latex-source:";

function markLatexSources(node: HastNode, sources: string[]): void {
  const children = node.children;
  if (!children) return;

  for (let index = 0; index < children.length; index += 1) {
    const child = children[index];
    const source = child.properties?.dataLatexSource;
    if (child.type === "element" && typeof source === "string") {
      const marker = `${SOURCE_MARKER}${sources.push(source) - 1}`;
      children.splice(index, 0, { type: "comment", value: marker });
      index += 1;
      continue;
    }
    markLatexSources(child, sources);
  }
}

function classNames(node: HastNode): string[] {
  const value = node.properties?.className;
  return Array.isArray(value) ? value.filter((item): item is string => typeof item === "string") : [];
}

function findKatexRoot(node: HastNode): HastNode | null {
  if (
    node.type === "element"
    && (classNames(node).includes("katex-display") || classNames(node).includes("katex"))
  ) {
    return node;
  }
  for (const child of node.children ?? []) {
    const root = findKatexRoot(child);
    if (root) return root;
  }
  return null;
}

function findTexAnnotation(node: HastNode): HastNode | null {
  if (
    node.type === "element"
    && node.tagName === "annotation"
    && node.properties?.encoding === "application/x-tex"
  ) {
    return node;
  }
  for (const child of node.children ?? []) {
    const annotation = findTexAnnotation(child);
    if (annotation) return annotation;
  }
  return null;
}

function restoreLatexSources(node: HastNode, sources: string[]): void {
  const children = node.children;
  if (!children) return;

  for (let index = 0; index < children.length;) {
    const child = children[index];
    const markerIndex = child.type === "comment" && child.value?.startsWith(SOURCE_MARKER)
      ? Number(child.value.slice(SOURCE_MARKER.length))
      : Number.NaN;
    if (Number.isInteger(markerIndex)) {
      const source = sources[markerIndex];
      const rendered = children[index + 1];
      const root = rendered ? findKatexRoot(rendered) : null;
      const annotation = root ? findTexAnnotation(root) : null;
      if (root && source !== undefined) {
        root.properties ??= {};
        root.properties.dataLatexSource = source;
      }
      if (annotation && source !== undefined) {
        annotation.children = [{ type: "text", value: source }];
      }
      children.splice(index, 1);
      continue;
    }
    restoreLatexSources(child, sources);
    index += 1;
  }
}

/**
 * Render the normalised TeX with KaTeX, then restore the original source into
 * the generated MathML annotation so selection-copy can round-trip user input.
 */
export function rehypeReasonixKatex() {
  const renderKatex = rehypeKatex();
  return (
    tree: Parameters<typeof renderKatex>[0],
    file: Parameters<typeof renderKatex>[1],
  ): undefined => {
    const sources: string[] = [];
    markLatexSources(tree as HastNode, sources);
    renderKatex(tree, file);
    restoreLatexSources(tree as HastNode, sources);
  };
}

export const reasonixRehypePlugins = [rehypeReasonixKatex];
