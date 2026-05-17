import { Readable } from "node:stream";

export function makeNullStdin(): NodeJS.ReadStream {
  const stream = new Readable({
    read() {},
  });
  Object.assign(stream, {
    isTTY: true,
    isRaw: false,
    setRawMode(_mode: boolean) {
      return stream;
    },
    setEncoding() {
      return stream;
    },
    pause() {
      return stream;
    },
    resume() {
      return stream;
    },
    isPaused() {
      return false;
    },
    ref() {},
    unref() {},
  });
  return stream as unknown as NodeJS.ReadStream;
}
