import type { DOMElement } from './dom.js';

type Measurement = {
  width: number;
  height: number;
};

/** Read the most-recently-computed pixel dimensions of a `<Box>` node. */
const measureElement = (node: DOMElement): Measurement => ({
  width: node.yogaNode?.getComputedWidth() ?? 0,
  height: node.yogaNode?.getComputedHeight() ?? 0,
});

export default measureElement;
