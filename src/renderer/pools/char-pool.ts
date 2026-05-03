const ASCII_TABLE_SIZE = 128;
const SPACE_ID = 0;
const EMPTY_ID = 1;

export class CharPool {
  private readonly strings: string[];
  private readonly map: Map<string, number>;
  private readonly ascii: Int32Array;

  constructor() {
    this.strings = [" ", ""];
    this.map = new Map([
      [" ", SPACE_ID],
      ["", EMPTY_ID],
    ]);
    this.ascii = new Int32Array(ASCII_TABLE_SIZE).fill(-1);
    this.ascii[" ".charCodeAt(0)] = SPACE_ID;
  }

  intern(s: string): number {
    if (s.length === 1) {
      const code = s.charCodeAt(0);
      if (code < ASCII_TABLE_SIZE) {
        const cached = this.ascii[code]!;
        if (cached !== -1) return cached;
        const id = this.strings.length;
        this.strings.push(s);
        this.ascii[code] = id;
        this.map.set(s, id);
        return id;
      }
    }
    const existing = this.map.get(s);
    if (existing !== undefined) return existing;
    const id = this.strings.length;
    this.strings.push(s);
    this.map.set(s, id);
    return id;
  }

  get(id: number): string {
    return this.strings[id] ?? " ";
  }

  get size(): number {
    return this.strings.length;
  }
}
