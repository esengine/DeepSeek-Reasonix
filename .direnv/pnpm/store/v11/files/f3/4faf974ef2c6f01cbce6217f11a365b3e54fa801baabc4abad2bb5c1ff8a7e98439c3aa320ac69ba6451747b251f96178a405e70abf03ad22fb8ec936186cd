import type { D3HtmlSelection, D3Selection } from '../types.js';
export declare const convert: (template: TemplateStringsArray, ...params: unknown[]) => {
    [k: string]: unknown;
}[];
export declare const MOCKED_BBOX: {
    x: number;
    y: number;
    width: number;
    height: number;
};
interface JsdomItInput {
    body: D3HtmlSelection<HTMLElement>;
    svg: D3Selection<SVGSVGElement>;
}
/**
 * Test method borrowed from d3 : https://github.com/d3/d3-selection/blob/v3.0.0/test/jsdom.js
 *
 * Fools d3 into thinking it's working in a browser with a real DOM.
 *
 * The DOM is actually an instance of JSDom with monkey-patches for DOM methods that require a
 * rendering engine.
 *
 * The resulting environment is capable of rendering SVGs with the caveat that layouts are
 * completely screwed.
 *
 * This makes it possible to make structural tests instead of mocking everything.
 */
export declare function jsdomIt(message: string, run: (input: JsdomItInput) => void | Promise<void>): void;
/**
 * Retrieves the node from its parent with ParentNode#querySelector,
 * then checks that it exists before returning it.
 */
export declare function ensureNodeFromSelector(selector: string, parent?: ParentNode): Element;
/**
 * Asserts that no element `id` attribute appears more than once within the given DOM subtree.
 * Useful for verifying that multiple mermaid diagrams rendered into the same document
 * do not produce colliding SVG element IDs.
 */
export declare function assertNoDuplicateIds(root: ParentNode): void;
export {};
