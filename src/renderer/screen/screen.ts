import { type Cell, EMPTY_CELL } from "./cell.js";

export interface Rectangle {
  x: number;
  y: number;
  width: number;
  height: number;
}

export class Screen {
  readonly width: number;
  readonly height: number;
  readonly cells: Cell[];
  private _damage: Rectangle | undefined;

  constructor(width: number, height: number) {
    this.width = Math.max(0, width | 0);
    this.height = Math.max(0, height | 0);
    this.cells = new Array<Cell>(this.width * this.height);
    for (let i = 0; i < this.cells.length; i++) this.cells[i] = EMPTY_CELL;
    this._damage = undefined;
  }

  get damage(): Rectangle | undefined {
    return this._damage;
  }

  resetDamage(): void {
    this._damage = undefined;
  }

  cellAt(x: number, y: number): Cell | undefined {
    if (x < 0 || x >= this.width || y < 0 || y >= this.height) return undefined;
    return this.cells[y * this.width + x];
  }

  writeCell(x: number, y: number, cell: Cell): void {
    if (x < 0 || x >= this.width || y < 0 || y >= this.height) return;
    this.cells[y * this.width + x] = cell;
    this.markDamage(x, y, 1, 1);
  }

  markDamage(x: number, y: number, w: number, h: number): void {
    if (w <= 0 || h <= 0) return;
    if (this._damage === undefined) {
      this._damage = { x, y, width: w, height: h };
      return;
    }
    const d = this._damage;
    const x1 = Math.min(d.x, x);
    const y1 = Math.min(d.y, y);
    const x2 = Math.max(d.x + d.width, x + w);
    const y2 = Math.max(d.y + d.height, y + h);
    d.x = x1;
    d.y = y1;
    d.width = x2 - x1;
    d.height = y2 - y1;
  }
}
