export type Patch =
  | { readonly type: "stdout"; readonly content: string }
  | { readonly type: "cursorMove"; readonly dx: number; readonly dy: number }
  | { readonly type: "cursorTo"; readonly col: number }
  | { readonly type: "cursorVisible"; readonly visible: boolean }
  | { readonly type: "carriageReturn" }
  | { readonly type: "styleStr"; readonly str: string }
  | { readonly type: "hyperlink"; readonly uri: string }
  | { readonly type: "clear"; readonly count: number }
  | { readonly type: "clearTerminal" };

export type Diff = ReadonlyArray<Patch>;
