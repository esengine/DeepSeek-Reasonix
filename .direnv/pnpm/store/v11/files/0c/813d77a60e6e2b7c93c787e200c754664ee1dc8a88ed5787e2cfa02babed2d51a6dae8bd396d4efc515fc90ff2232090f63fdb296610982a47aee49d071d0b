/**
 * Box-drawing pre-processor for treeView diagrams.
 *
 * Converts box-drawing character input (├──, └──, │) to indent-based input
 * before Langium parsing. Supports both standard and heavy Unicode variants.
 */
export interface PreprocessResult {
    /** The (possibly transformed) input text */
    text: string;
    /** Maps output line numbers (1-based) → original line numbers (1-based). Empty if no transformation. */
    lineMap: Map<number, number>;
}
/**
 * Detects whether any of the given lines contain box-drawing characters.
 */
export declare function isBoxDrawingFormat(lines: string[]): boolean;
/**
 * Remaps line numbers in an error message from output line numbers to original line numbers.
 */
export declare function remapErrorLines(message: string, lineMap: Map<number, number>): string;
/**
 * Pre-processes box-drawing formatted treeView input into indent-based format.
 *
 * If the input uses box-drawing characters (├── └── │ or heavy variants ┣━━ ┗━━ ┃),
 * it is converted to indentation-based format that Langium can parse directly.
 * If the input is already indent-based, it is returned unchanged.
 *
 * @returns The transformed text and a line mapping for error remapping.
 */
export declare function preprocessBoxDrawing(input: string): PreprocessResult;
