import { access } from "node:fs/promises";
import path from "node:path";
import { preview } from "vite";

export async function startPreviewServer(root, port) {
  const entry = path.join(root, "dist", "index.html");
  await access(entry).catch((cause) => {
    throw new Error(`Browser gate requires the built frontend at ${entry}; build it or download the frontend artifact into dist.`, { cause });
  });
  return preview({
    root,
    logLevel: "silent",
    preview: { host: "127.0.0.1", port, strictPort: true },
  });
}
