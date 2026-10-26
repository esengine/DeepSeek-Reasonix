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
  textGlow?: "default" | "none";
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

// What a pack says about shape and rhythm rather than colour. The stylesheet
// owns what a radius or a gutter measures; a recipe only says how far it steps.
export interface ThemeRecipes {
  density?: "compact" | "comfortable";
  corners?: "square" | "soft" | "round";
}

export interface ThemePack {
  id: string;
  name: string;
  author?: string;
  description?: string;
  active?: boolean;
  tokens: { light?: Record<string, string>; dark?: Record<string, string> };
  recipes?: ThemeRecipes;
  background?: ThemeBackground;
  sky?: ThemeSky;
  hasPreview?: boolean;
  removable?: boolean;
  // Values the kernel dropped and why: an unknown name, or a value that could
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
export interface ZoomRange {
  min: number;
  max: number;
  step: number;
}

export interface Appearance {
  // "zh" | "en" | "" to follow the machine. The interface's language only —
  // what the model answers in follows each message you write.
  language?: string;
  zoom?: number;
  // What the kernel holds zoom to; the slider and the keyboard steps read it
  // rather than carrying a copy.
  zoomRange?: ZoomRange;
  readSize?: number;
  // Interface text alone, as a scale over the stylesheet's sizes; zoom moves
  // spacing and controls with it, this does not.
  textSize?: number;
  fontUi?: string;
  fontMono?: string;
  // How far prose may run: "standard" holds it to a reading line length,
  // "full" lets it use the window. Cards, commands and output are not
  // governed by it.
  width?: string;
  wallpaper?: Wallpaper;
}

export interface WallpaperLayer {
  url: string;
  x: number;
  y: number;
  w: number;
  h: number;
  alpha: number;
  effects?: string[];
}

export interface WallpaperAudioTrack {
  url: string;
  volume: number;
  loop: boolean;
}

export interface Wallpaper {
  // Content-addressed, so the bytes at this address never change.
  url: string;
  // A picture is drawn as the window's background, a video as an element
  // behind it; a scene adds its soundtrack and pointer parallax. The kernel
  // decides which from the type it was uploaded as.
  kind: "image" | "video" | "scene";
  // A Wallpaper Engine scene's title and soundtrack, when it has one.
  title?: string;
  audio?: string;
  audios?: string[];
  audioTracks?: WallpaperAudioTrack[];
  layers?: WallpaperLayer[];
  sceneWidth?: number;
  sceneHeight?: number;
  parallax?: number;
  muted?: boolean;
  volume?: number;
  opacity: number;
  dim: number;
  focusX: number;
  focusY: number;
}

export interface SceneFolder {
  project: File;
  scene: File;
  preview?: File;
}
