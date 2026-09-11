import assert from "node:assert/strict";
import { test } from "node:test";
import { LineDecoder, OversizeFrameError, RpcClient, RpcError } from "./rpc.js";

function harness(limit?: number) {
  const written: string[] = [];
  const requests: Array<{ method: string; params: unknown }> = [];
  const notifications: Array<{ method: string; params: unknown }> = [];
  const errors: string[] = [];
  const client = new RpcClient(
    { write: (line) => written.push(line) },
    {
      onRequest: async (method, params) => {
        requests.push({ method, params });
        if (method === "host/fail") throw new RpcError(-32601, "nope");
        return { echoed: params };
      },
      onNotification: (method, params) => notifications.push({ method, params }),
      onProtocolError: (kind) => errors.push(kind),
    },
    limit,
  );
  const feed = (text: string) => client.feed(Buffer.from(text));
  const lastFrame = () => JSON.parse(written[written.length - 1] as string) as Record<string, unknown>;
  return { client, written, requests, notifications, errors, feed, lastFrame };
}

test("LineDecoder reassembles partial lines and strips CR", () => {
  const decoder = new LineDecoder();
  assert.deepEqual(decoder.push(Buffer.from('{"a":')), []);
  assert.deepEqual(decoder.push(Buffer.from('1}\r\n{"b":2}\n{"c"')), ['{"a":1}', '{"b":2}']);
  assert.deepEqual(decoder.push(Buffer.from(":3}\n")), ['{"c":3}']);
});

test("LineDecoder rejects a line above the limit, with and without a newline", () => {
  const decoder = new LineDecoder(8);
  assert.throws(() => decoder.push(Buffer.from("0123456789\n")), OversizeFrameError);
  assert.deepEqual(decoder.push(Buffer.from("ok\n")), ["ok"], "the decoder resynchronises after the failure");
  assert.throws(() => decoder.push(Buffer.from("0123456789")), OversizeFrameError);
});

test("responses resolve their own request regardless of arrival order", async () => {
  const h = harness();
  const first = h.client.request("desktop/invoke", { method: "A" });
  const second = h.client.request("desktop/invoke", { method: "B" });
  assert.equal(h.written.length, 2);
  h.feed('{"jsonrpc":"2.0","id":2,"result":"b"}\n{"jsonrpc":"2.0","id":1,"result":"a"}\n');
  assert.equal(await second, "b");
  assert.equal(await first, "a");
  assert.equal(h.client.pendingCount, 0);
});

test("non-JSON and non-2.0 lines are counted and ignored", () => {
  const h = harness();
  h.feed("plain log line\n{\"id\":1,\"result\":true}\n\n");
  assert.equal(h.client.stats.ignoredLines, 2);
  assert.deepEqual(h.errors, ["non-json", "invalid"]);
});

test("a response arriving after the timeout is ignored", async () => {
  const h = harness();
  await assert.rejects(h.client.request("desktop/start", {}, 5), (error: unknown) => error instanceof RpcError && /timed out/.test(error.message));
  h.feed('{"jsonrpc":"2.0","id":1,"result":{}}\n');
  assert.equal(h.client.stats.orphanResponses, 1);
  assert.deepEqual(h.errors, ["orphan-response"]);
});

test("error responses reject with the service's code and message", async () => {
  const h = harness();
  const pending = h.client.request("desktop/hello", {});
  h.feed('{"jsonrpc":"2.0","id":1,"error":{"code":-32003,"message":"contract digest differs","data":{"x":1}}}\n');
  await assert.rejects(pending, (error: unknown) => error instanceof RpcError && error.code === -32003 && error.message === "contract digest differs");
});

test("reverse requests are answered and notifications dispatched", async () => {
  const h = harness();
  h.feed('{"jsonrpc":"2.0","id":"r1","method":"host/window.show","params":{"reason":"tray"}}\n');
  h.feed('{"jsonrpc":"2.0","method":"desktop/event","params":{"seq":1}}\n');
  await new Promise((resolve) => setImmediate(resolve));
  assert.deepEqual(h.requests, [{ method: "host/window.show", params: { reason: "tray" } }]);
  assert.deepEqual(h.lastFrame(), { jsonrpc: "2.0", id: "r1", result: { echoed: { reason: "tray" } } });
  assert.deepEqual(h.notifications, [{ method: "desktop/event", params: { seq: 1 } }]);
  h.feed('{"jsonrpc":"2.0","id":7,"method":"host/fail","params":{}}\n');
  await new Promise((resolve) => setImmediate(resolve));
  assert.deepEqual(h.lastFrame(), { jsonrpc: "2.0", id: 7, error: { code: -32601, message: "nope" } });
});

test("an oversize frame closes the client and rejects everything in flight", async () => {
  const h = harness(32);
  const pending = h.client.request("desktop/invoke", {});
  assert.throws(() => h.feed("x".repeat(64) + "\n"), OversizeFrameError);
  await assert.rejects(pending, OversizeFrameError);
  assert.equal(h.client.closed, true);
  await assert.rejects(h.client.request("desktop/invoke", {}), OversizeFrameError);
});
