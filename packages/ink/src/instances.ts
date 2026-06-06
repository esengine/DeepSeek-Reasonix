import type Ink from './ink.js';

const instances = new Map<NodeJS.WriteStream, Ink>();

export default instances;
