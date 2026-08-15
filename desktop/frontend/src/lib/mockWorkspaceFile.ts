import type { FilePreview } from "./types";

const WORKSPACE_FILE_SAMPLES: Record<string, string> = {
  "README.md": "# Reasonix\n\nBrowser-dev workspace preview.\n\n- Chat in the center\n- Browse files on the right\n- Keep sessions on the left\n",
  "go.mod": "module reasonix\n\ngo 1.23\n",
  "desktop/file.go": "package desktop\n\nfunc main() {\n\tprintln(\"workspace preview\")\n}\n",
  "internal/event.go": "package internal\n\n// mock file used by the browser dev seam\n",
};

export function mockWorkspaceFile(path: string): FilePreview {
  const sample = WORKSPACE_FILE_SAMPLES[path];
  const body = sample ?? `// ${path}\n\nMock file body from browser dev.`;
  return { path, body, size: sample?.length ?? 42, truncated: false, binary: false };
}
