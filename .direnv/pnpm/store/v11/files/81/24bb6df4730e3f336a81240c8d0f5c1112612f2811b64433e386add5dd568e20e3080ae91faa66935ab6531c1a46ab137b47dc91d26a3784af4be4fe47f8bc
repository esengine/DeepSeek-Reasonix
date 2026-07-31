export default createLabel;
/**
 * @param {import('../../types.js').D3Selection<SVGGElement>} element - The parent element to which the label will be appended.
 * @param {string | [string] | undefined} _vertexText - The text content of the label.
 * @param {string} style
 * @param {boolean} [isTitle] - If `true`, style this as a title label, else as a normal label.
 * @param {boolean} [isNode] - If `true`, style this as a node label, else as an edge label.
 * @deprecated svg-util/createText instead
 *
 * @example
 *
 * If `getEffectiveHtmlLabels(getConfig())` is `true`, you must reset the width
 * and height of the created label after creation, like this:
 *
 * ```js
 * const labelElement = await createLabel(parent, ... );
 * let slBox = labelElement.getBBox();
 * if (useHtmlLabels) {
 *   const div = labelElement.children[0];
 *   const dv = select(labelElement);
 *   slBox = div.getBoundingClientRect();
 *   dv.attr('width', slBox.width);
 *   dv.attr('height', slBox.height);
 * }
 * parent.attr('transform', 'translate(' + -slBox.width / 2 + ', ' + -slBox.height / 2 + ')');
 * ```
 */
declare function createLabel(element: import("../../types.js").D3Selection<SVGGElement>, _vertexText: string | [string] | undefined, style: string, isTitle?: boolean, isNode?: boolean): Promise<SVGGElement>;
