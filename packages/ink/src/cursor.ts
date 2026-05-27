/** Position of the terminal hardware cursor in screen-buffer coordinates. */
export type Cursor = {
  readonly x: number;
  readonly y: number;
  readonly visible: boolean;
};
