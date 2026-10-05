// look.ts — what the window is set in: a pack somebody authored, and the
// settings the reader made for themselves on top of it. They are kept apart
// because they answer to different people, and only the second one can be
// changed from inside the app.

export interface ThemeBackground {
  image: boolean;
  focusX: number;
  focusY: number;
  safeArea?: "left" | "center" | "right";
  // The same picture at rest and at work — a photo behind a transcript being
  // read is in the way, so a pack says how far it should recede.
  homeOpacity: number;
  taskOpacity: number;
  overlayStrength: number;
}

// A live backdrop the window draws, rather than a picture it places. Absent on
// a pack that does not want one; the whole layer is then never mounted.
export interface ThemeSky {
  ray?: string;
  cloud?: string;
  cloudLit?: string;
  rayAlpha: number;
  cloudAlpha: number;
}

export interface ThemePack {
  id: string;
  name: string;
  author?: string;
  description?: string;
  active?: boolean;
  tokens: { light?: Record<string, string>; dark?: Record<string, string> };
  background?: ThemeBackground;
  sky?: ThemeSky;
  hasPreview?: boolean;
  // Tokens the kernel dropped and why: an unknown name, or a value that could
  // not be let into a stylesheet. The pack still loads without them.
  warnings?: string[];
}

// What an import installed, and the files it did not read because a pack has
// no place for them.
export interface ThemeImport {
  pack: ThemePack;
  ignored?: string[];
}

// What the user set for themselves, over whatever pack is active. Zero and ""
// mean "unset" rather than a value, so an untouched install draws from the
// stylesheet instead of from numbers written into a config.
export interface Appearance {
  // "zh" | "en" | "" to follow the machine. The interface's language only —
  // what the model answers in follows each message you write.
  language?: string;
  zoom?: number;
  readSize?: number;
  fontUi?: string;
  fontMono?: string;
  // How far prose may run: "standard" holds it to a reading line length,
  // "full" lets it use the window. Cards, commands and output are not
  // governed by it.
  width?: string;
  wallpaper?: Wallpaper;
}

export interface Wallpaper {
  // Content-addressed, so the bytes at this address never change.
  url: string;
  opacity: number;
  dim: number;
  focusX: number;
  focusY: number;
}
