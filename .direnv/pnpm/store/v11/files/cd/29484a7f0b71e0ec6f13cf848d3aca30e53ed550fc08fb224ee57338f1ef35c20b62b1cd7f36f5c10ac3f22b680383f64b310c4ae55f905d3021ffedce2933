import type { MermaidConfig } from '../config.type.js';
import type { SVGGroup } from '../diagram-api/types.js';
import type { D3Selection } from '../types.js';
export declare function computeDimensionOfText(parentNode: SVGGroup, lineHeight: number, text: string): DOMRect | undefined;
/**
 * Convert fontawesome labels into fontawesome icons by using a regex pattern
 * @param text - The raw string to convert
 * @param config - Mermaid config
 * @returns string with fontawesome icons as svg if the icon is registered otherwise as i tags
 */
export declare function replaceIconSubstring(text: string, config?: MermaidConfig): Promise<string>;
/**
 * Creates a text element within the given SVG group element.
 *
 * If `markdown` is `true`, basic markdown syntax will be processed.
 * Otherwise, if:
 *   - `useHtmlLabels` is `true`, the text will be sanitized and set in `<foreignObject>` as HTML.
 *   - `useHtmlLabels` is `false`, the text will be added as a `<text>` element using `.text`
 *
 * @param el - The parent SVG `<g>` element to append the text element to.
 * @param text - The text content to be displayed.
 * @param options - Optional options
 * @param config - Mermaid configuration object
 * @returns The created text element, either a `<foreignObject>` or a `<text>` element depending on the options.
 */
export declare const createText: (el: D3Selection<SVGGElement>, text?: string, { style, isTitle, classes, useHtmlLabels, markdown, isNode, width, addSvgBackground, }?: {
    style?: string | undefined;
    isTitle?: boolean | undefined;
    classes?: string | undefined;
    useHtmlLabels?: boolean | undefined;
    markdown?: boolean | undefined;
    isNode?: boolean | undefined;
    width?: number | undefined;
    addSvgBackground?: boolean | undefined;
}, config?: MermaidConfig) => Promise<SVGGElement>;
