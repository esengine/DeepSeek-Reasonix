import type { MarkdownLine } from './types.js';
import type { MermaidConfig } from '../config.type.js';
/**
 * @param nonMarkdownText - Non-markdown text to split into plain-text formatted lines.
 * This treats new lines, `\n`, and `<br/>` as line breaks, and splits on spaces for words.
 * SVG tags are preserved as separate words to maintain proper formatting.
 */
export declare function nonMarkdownToLines(nonMarkdownText: string): MarkdownLine[];
/**
 * @param markdown - markdown to split into lines
 */
export declare function markdownToLines(markdown: string, config?: MermaidConfig): MarkdownLine[];
/**
 * Counterpart to {@link markdownToHTML} for non-markdown text.
 *
 * Non-markdown text is not wrapped normally, and users can use an explicit `\n`
 * sequence to add a line break.
 *
 * @param text - Non-markdown text to convert to HTML.
 */
export declare function nonMarkdownToHTML(text: string): string;
export declare function markdownToHTML(markdown: string, { markdownAutoWrap }?: MermaidConfig): string;
