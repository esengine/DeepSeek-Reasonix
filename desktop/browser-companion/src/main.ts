// Reasonix Browser Companion main process. Lifecycle is host-driven: the
// Reasonix desktop host spawns this process, handshakes over stdin/stdout
// (length-prefixed JSON frames), and owns the window lifecycle. Without a
// hello from the host the companion stays headless; when the host pipe closes
// the companion exits so no orphan process survives the host.
//
// All application logic lives in CompanionApp (src/app.ts); this file only
// connects the wire (stdin/stdout) and the Electron lifecycle to it.

import { app } from "electron";
import * as fs from "node:fs";
import * as path from "node:path";
import {
  FrameReader,
  ProtocolError,
  PROTOCOL_VERSION,
  responseError,
  validateRequest,
  writeFrame,
  type BrowserRequest,
  type BrowserResponse,
} from "./protocol";
import { CompanionApp } from "./app";
import {
  BROWSER_SCHEMA_HASH,
  type BrowserErrorCode,
} from "./generated/browserProtocol.generated";

const componentVersion = JSON.parse(
  fs.readFileSync(path.join(__dirname, "..", "package.json"), "utf8"),
).version as string;

// ---- wire plumbing ---------------------------------------------------------

const reader = new FrameReader();

const companion = new CompanionApp({
  componentVersion,
  electronVersion: process.versions.electron ?? "",
  chromiumVersion: process.versions.chrome ?? "",
  pid: process.pid,
  emitEvent: (name, ownerId, data) => {
    process.stdout.write(
      writeFrame(JSON.stringify({ protocolVersion: PROTOCOL_VERSION, event: { name, ownerId, data } })),
    );
  },
  exit: (code) => app.exit(code),
});

function send(resp: BrowserResponse): void {
  process.stdout.write(writeFrame(JSON.stringify(resp)));
}

// ---- lifecycle -------------------------------------------------------------

app.whenReady().then(() => {
  companion.markReady();
  // The wire is consumed only after Electron is ready: if hello arrived
  // first (buffered by the stdin stream), markReady has already opened the
  // window, so the successful handshake is never followed by not_ready
  // tab.open responses (the Go host restores immediately after hello).
  process.stdin.on("data", (chunk: Buffer) => {
  let frames: Buffer[];
  try {
    frames = reader.feed(chunk);
  } catch (err) {
    // Protocol violation: the peer is hostile or the wire is corrupt. Exit
    // rather than trying to recover an untrusted stream.
    const code: BrowserErrorCode = err instanceof ProtocolError ? err.code : "internal";
    try {
      process.stdout.write(writeFrame(JSON.stringify(responseError("", code, (err as Error).message))));
    } catch {
      // stdout may already be closed; nothing left to do.
    }
    app.exit(2);
    return;
  }
  for (const payload of frames) {
    let req: BrowserRequest;
    try {
      req = validateRequest(JSON.parse(payload.toString("utf8")));
    } catch (err) {
      const code: BrowserErrorCode =
        err instanceof ProtocolError ? err.code : "invalid_params";
      process.stdout.write(
        writeFrame(JSON.stringify(responseError("", code, (err as Error).message))),
      );
      continue;
    }
    // Requests are intentionally dispatched concurrently so request.cancel
    // can interrupt a pending tab.wait. Each response is still written as one
    // complete length-prefixed buffer, so frames cannot interleave.
    void companion.handle(req).then(send, (err: unknown) => {
      const code: BrowserErrorCode =
        err instanceof ProtocolError ? err.code : "internal";
      send(responseError(req.requestId, code, (err as Error).message));
    });
  }
  });
});

process.stdin.on("end", () => {
  // Host pipe closed: the host is gone or shutting down. Never linger.
  app.exit(0);
});

process.stdin.on("error", () => {
  app.exit(0);
});

process.on("SIGTERM", () => {
  app.exit(0);
});

process.on("SIGINT", () => {
  app.exit(0);
});

// The schema hash pins this build to the exact wire document the Go host
// compiled against; the companion test suite checks the committed generated
// file, and this constant keeps it reachable at runtime for diagnostics.
void BROWSER_SCHEMA_HASH;
