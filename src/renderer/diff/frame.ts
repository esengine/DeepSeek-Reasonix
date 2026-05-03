import { Screen } from "../screen/screen.js";

export interface Cursor {
  readonly x: number;
  readonly y: number;
  readonly visible: boolean;
}

export interface Frame {
  readonly screen: Screen;
  readonly viewportWidth: number;
  readonly viewportHeight: number;
  readonly cursor: Cursor;
}

export function emptyFrame(viewportWidth: number, viewportHeight: number): Frame {
  return {
    screen: new Screen(0, 0),
    viewportWidth,
    viewportHeight,
    cursor: { x: 0, y: 0, visible: true },
  };
}
