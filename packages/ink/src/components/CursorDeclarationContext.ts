import { createContext } from 'react';
import type { DOMElement } from '../dom.js';

export type CursorDeclaration = {
  /** Column offset (in terminal cells) relative to `node`'s top-left. */
  readonly relativeX: number;
  /** Row offset (in lines) relative to `node`'s top-left. */
  readonly relativeY: number;
  /** The owning `ink-box` element. */
  readonly node: DOMElement;
};

/** Setter for the declared native-cursor position. */
export type CursorDeclarationSetter = (
  declaration: CursorDeclaration | null,
  clearIfNode?: DOMElement | null,
) => void;

const CursorDeclarationContext = createContext<CursorDeclarationSetter>(
  () => {},
);

export default CursorDeclarationContext;
