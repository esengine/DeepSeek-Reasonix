import { Writable } from "node:stream";

export function makeNullStdout(real: NodeJS.WriteStream = process.stdout): NodeJS.WriteStream {
  const writable = new Writable({
    write(_chunk, _encoding, callback) {
      callback();
    },
  });
  // Ink consumers — every <Box>/<Text>/useStdout — subscribe to the stdout's
  // `resize` event. The App tree has 11+ such subscribers, which trips Node's
  // default 10-listener leak warning. The warning text then writes to stderr
  // and corrupts the alt-screen the Rust child draws (the user sees garbage
  // mid-row). Raise the cap above the realistic subscriber count.
  writable.setMaxListeners(50);
  applyDimensions(writable, real);
  if (typeof real.on === "function") {
    real.on("resize", () => {
      applyDimensions(writable, real);
      writable.emit("resize");
    });
  }
  return writable as unknown as NodeJS.WriteStream;
}

function applyDimensions(target: Writable, real: NodeJS.WriteStream): void {
  Object.assign(target, {
    columns: real.columns ?? 80,
    rows: real.rows ?? 24,
    isTTY: true,
  });
}
