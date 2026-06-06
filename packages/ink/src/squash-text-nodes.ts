import type { DOMElement } from './dom.js';
import type { TextStyles } from './styles.js';

/** A run of text with the styles that should be applied to it. */
export type StyledSegment = {
  text: string;
  styles: TextStyles;
  hyperlink?: string;
};

/** Walk a text-bearing subtree and flatten it into a list of styled runs. */
export function squashTextNodesToSegments(
  node: DOMElement,
  inheritedStyles: TextStyles = {},
  inheritedHyperlink?: string,
  out: StyledSegment[] = [],
): StyledSegment[] {
  const mergedStyles = node.textStyles
    ? { ...inheritedStyles, ...node.textStyles }
    : inheritedStyles;

  for (const child of node.childNodes) {
    if (child === undefined) continue;

    if (child.nodeName === '#text') {
      if (child.nodeValue.length > 0) {
        out.push({
          text: child.nodeValue,
          styles: mergedStyles,
          hyperlink: inheritedHyperlink,
        });
      }
      continue;
    }

    if (child.nodeName === 'ink-text' || child.nodeName === 'ink-virtual-text') {
      squashTextNodesToSegments(child, mergedStyles, inheritedHyperlink, out);
      continue;
    }

    if (child.nodeName === 'ink-link') {
      const href = child.attributes['href'] as string | undefined;
      squashTextNodesToSegments(child, mergedStyles, href || inheritedHyperlink, out);
    }
  }

  return out;
}

function squashTextNodes(node: DOMElement): string {
  let text = '';

  for (const child of node.childNodes) {
    if (child === undefined) continue;

    if (child.nodeName === '#text') {
      text += child.nodeValue;
    } else if (
      child.nodeName === 'ink-text' ||
      child.nodeName === 'ink-virtual-text' ||
      child.nodeName === 'ink-link'
    ) {
      text += squashTextNodes(child);
    }
  }

  return text;
}

export default squashTextNodes;
