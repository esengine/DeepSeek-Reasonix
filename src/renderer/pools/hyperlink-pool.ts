const NO_HYPERLINK = 0;

export class HyperlinkPool {
  private readonly strings: string[] = [""];
  private readonly map = new Map<string, number>();

  intern(uri: string | undefined): number {
    if (!uri) return NO_HYPERLINK;
    const existing = this.map.get(uri);
    if (existing !== undefined) return existing;
    const id = this.strings.length;
    this.strings.push(uri);
    this.map.set(uri, id);
    return id;
  }

  get(id: number): string | undefined {
    if (id === NO_HYPERLINK) return undefined;
    return this.strings[id];
  }

  get size(): number {
    return this.strings.length;
  }
}
