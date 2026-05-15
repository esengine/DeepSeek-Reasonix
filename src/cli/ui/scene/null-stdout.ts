import { Writable } from "node:stream";

export function makeNullStdout(real: NodeJS.WriteStream = process.stdout): NodeJS.WriteStream {
  const writable = new Writable({
    write(_chunk, _encoding, callback) {
      callback();
    },
  });
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
