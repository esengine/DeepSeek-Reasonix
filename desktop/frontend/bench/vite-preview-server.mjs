import { preview } from "vite";

export function startPreviewServer(root, port) {
  return preview({
    root,
    logLevel: "silent",
    preview: { host: "127.0.0.1", port, strictPort: true },
  });
}
