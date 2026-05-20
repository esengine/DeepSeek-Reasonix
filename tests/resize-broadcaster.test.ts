import { EventEmitter } from "node:events";
import type { WriteStream } from "node:tty";
import { afterEach, describe, expect, it } from "vitest";
import {
  _resizeSubscriberCount,
  _uninstallResizeBroadcaster,
  installResizeBroadcaster,
} from "../src/cli/ui/resize-broadcaster.js";

function fakeStdout(): WriteStream {
  const ee = new EventEmitter();
  ee.setMaxListeners(10);
  return ee as unknown as WriteStream;
}

afterEach(() => {
  _uninstallResizeBroadcaster();
});

describe("resize broadcaster", () => {
  it("collapses N virtual subscribers into a single native resize listener", () => {
    const stdout = fakeStdout();
    installResizeBroadcaster(stdout);

    for (let i = 0; i < 50; i++) {
      stdout.on("resize", () => {});
    }

    expect(_resizeSubscriberCount()).toBe(50);
    expect((stdout as unknown as EventEmitter).listenerCount("resize")).toBe(1);
  });

  it("broadcasts emit to every virtual subscriber", () => {
    const stdout = fakeStdout();
    installResizeBroadcaster(stdout);
    const seen: number[] = [];
    for (let i = 0; i < 5; i++) {
      stdout.on("resize", () => seen.push(i));
    }

    (stdout as unknown as EventEmitter).emit("resize");

    expect(seen).toEqual([0, 1, 2, 3, 4]);
  });

  it("off() drops the subscriber so subsequent emits skip it", () => {
    const stdout = fakeStdout();
    installResizeBroadcaster(stdout);
    let aCount = 0;
    let bCount = 0;
    const a = (): void => {
      aCount++;
    };
    const b = (): void => {
      bCount++;
    };
    stdout.on("resize", a);
    stdout.on("resize", b);

    (stdout as unknown as EventEmitter).emit("resize");
    stdout.off("resize", a);
    (stdout as unknown as EventEmitter).emit("resize");

    expect(aCount).toBe(1);
    expect(bCount).toBe(2);
  });

  it("does not touch listeners for other events", () => {
    const stdout = fakeStdout();
    installResizeBroadcaster(stdout);
    let dataCount = 0;
    stdout.on("data", () => {
      dataCount++;
    });

    expect((stdout as unknown as EventEmitter).listenerCount("data")).toBe(1);
    (stdout as unknown as EventEmitter).emit("data");
    expect(dataCount).toBe(1);
  });

  it("install is idempotent — calling twice does not double-shim", () => {
    const stdout = fakeStdout();
    installResizeBroadcaster(stdout);
    installResizeBroadcaster(stdout);

    stdout.on("resize", () => {});
    expect((stdout as unknown as EventEmitter).listenerCount("resize")).toBe(1);
  });
});
