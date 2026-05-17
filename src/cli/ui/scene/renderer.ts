import { existsSync } from "node:fs";
import { createRequire } from "node:module";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

export type RustEvent =
  | { event: "submit"; text: string }
  | { event: "interrupt" }
  | { event: "exit" }
  | { event: "approval-response"; kind: string; choice: unknown }
  | { event: "composer"; text: string }
  | { event: "mode-set"; value: "review" | "auto" | "yolo" }
  | { event: "preset-set"; value: "auto" | "flash" | "pro" }
  | { event: "prompt-response"; id: string; text?: string; cancelled?: boolean }
  | { event: "list-picker-response"; id: string; key?: string; cancelled?: boolean }
  | { event: "setup-submit"; text: string };

export type Renderer = {
  emit(message: unknown): void;
  close(): Promise<void>;
};

export type CreateRendererOptions = {
  onEvent?: (event: RustEvent) => void;
};

type NativeRenderer = {
  emit(message: string): void;
  close(): void;
};

type NativeBinding = {
  createRenderer(onEvent: (eventJson: string) => void): NativeRenderer;
  hello?: () => string;
};

const TRIPLES: Record<string, string> = {
  "win32-x64": "win32-x64-msvc",
  "win32-arm64": "win32-arm64-msvc",
  "darwin-arm64": "darwin-arm64",
  "darwin-x64": "darwin-x64",
  "linux-x64": "linux-x64-gnu",
  "linux-arm64": "linux-arm64-gnu",
};

const requireCJS = createRequire(import.meta.url);
let cachedBinding: NativeBinding | null = null;

function tripleFor(platform: NodeJS.Platform, arch: string): string | null {
  return TRIPLES[`${platform}-${arch}`] ?? null;
}

function findDevTree(): string | null {
  let dir = dirname(fileURLToPath(import.meta.url));
  for (let i = 0; i < 12; i++) {
    if (existsSync(join(dir, "crates", "reasonix-render", "Cargo.toml"))) return dir;
    const parent = dirname(dir);
    if (parent === dir) break;
    dir = parent;
  }
  return null;
}

function loadBinding(): NativeBinding {
  if (cachedBinding) return cachedBinding;
  const { platform, arch } = process;
  const triple = tripleFor(platform, arch);
  if (!triple) {
    throw new Error(
      `reasonix-render: unsupported platform ${platform}-${arch}. Supported: ${Object.keys(TRIPLES).join(", ")}.`,
    );
  }
  const devTree = findDevTree();
  if (devTree) {
    const devNode = join(devTree, "crates", "reasonix-render", `reasonix-render.${triple}.node`);
    if (existsSync(devNode)) {
      cachedBinding = requireCJS(devNode) as NativeBinding;
      return cachedBinding;
    }
  }
  // Dynamic package name keeps tsup's static analyzer from trying to bundle it.
  const pkg = `@reasonix/render-${platform}-${arch}`;
  try {
    cachedBinding = requireCJS(pkg) as NativeBinding;
    return cachedBinding;
  } catch (err) {
    const reason = err instanceof Error ? err.message : String(err);
    throw new Error(
      `reasonix-render: no native binding found for ${platform}-${arch}. ` +
        `Tried dev tree + ${pkg}. Run \`npm run build:rust\` for source builds, ` +
        `or reinstall reasonix so npm fetches the optional dep. (${reason})`,
    );
  }
}

/** Probe whether the platform-specific native binding can be loaded. Lets
 * callers gracefully fall back to the Node/Ink TUI when the .node file is
 * missing (e.g., a platform we don't yet build for, or a corrupted install). */
export function isRendererAvailable(): boolean {
  try {
    loadBinding();
    return true;
  } catch {
    return false;
  }
}

export function createRenderer(opts: CreateRendererOptions = {}): Renderer {
  const binding = loadBinding();
  const handle = binding.createRenderer((eventJson: string) => {
    if (!opts.onEvent) return;
    try {
      const parsed = JSON.parse(eventJson) as RustEvent;
      if (parsed && typeof parsed.event === "string") opts.onEvent(parsed);
    } catch {
      // malformed event line — ignore
    }
  });
  let closed = false;
  return {
    emit(message: unknown): void {
      if (closed) return;
      handle.emit(JSON.stringify(message));
    },
    async close(): Promise<void> {
      if (closed) return;
      closed = true;
      handle.close();
    },
  };
}
