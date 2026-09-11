import { createServer, type Server } from "node:net";
import { createHash, randomUUID } from "node:crypto";
import type { Logger } from "./log.js";

export const QUIT_REQUEST = "--reasonix-lifecycle-request=quit";
export const STATUS_LIMIT = 16 * 1024;
export interface ShellStatus {
  schemaVersion: 1;
  product: "com.reasonix.desktop";
  pid: number;
  version: string;
  generation: string;
  homeKey: string;
  lifecycle: "starting" | "ready" | "failed" | "quitting" | "done";
  service: string;
  servicePID: number;
  visible: boolean;
  rendererVersion: string;
  healthy: boolean;
}

export function homeKey(profile: string): string {
  return createHash("sha256").update(profile.replaceAll("/", "\\").toLowerCase()).digest("hex");
}

export function initialShellStatus(profile: string, version: string): ShellStatus {
  return { schemaVersion: 1, product: "com.reasonix.desktop", pid: process.pid, version, generation: randomUUID(), homeKey: homeKey(profile), lifecycle: "starting", service: "starting", servicePID: 0, visible: false, rendererVersion: "", healthy: false };
}

export function listenShellStatus(snapshot: () => ShellStatus, log: Logger, address = `\\\\.\\pipe\\reasonix-shell-v1-${process.pid}`): Server {
  const server = createServer((socket) => {
    socket.on("error", () => undefined);
    socket.setTimeout(2000, () => socket.destroy());
    try {
      const data = JSON.stringify(snapshot()) + "\n";
      if (Buffer.byteLength(data) > STATUS_LIMIT) { socket.destroy(); return; }
      socket.end(data);
    } catch { socket.destroy(); }
  });
  server.maxConnections = 8;
  server.on("error", (error) => log.error(`shell status endpoint: ${error.message}`));
  server.listen(address);
  return server;
}
