"use strict";
const http = require("node:http");
const { TOKEN_COOKIE } = require("./hostclient");

const RETRY_MS = 1000;
const MAX_RETRY_MS = 10000;

// sseData splits what arrived on the stream into complete data payloads and
// the unfinished remainder. Comments and blank keep-alives carry nothing.
function sseData(buffered) {
  const out = [];
  let rest = buffered;
  for (;;) {
    const end = rest.indexOf("\n\n");
    if (end < 0) break;
    const block = rest.slice(0, end);
    rest = rest.slice(end + 2);
    const data = block
      .split("\n")
      .filter((l) => l.startsWith("data: "))
      .map((l) => l.slice(6))
      .join("\n");
    if (data) out.push(data);
  }
  return { data: out, rest };
}

// startBrowserRelay holds the kernel's browser stream open for as long as the
// window lives, and posts frames back one request at a time: an event that
// arrived before a reply has to reach the kernel before it.
function startBrowserRelay({ client, onFrame, onDrop }) {
  let stopped = false;
  let retry = RETRY_MS;
  let request = null;
  const queue = [];
  let posting = false;

  const connect = () => {
    if (stopped) return;
    const url = new URL("/browser-host/stream", client.origin);
    request = http.get(
      { hostname: url.hostname, port: url.port, path: url.pathname, headers: { cookie: `${TOKEN_COOKIE}=${client.token}` } },
      (res) => {
        if (res.statusCode !== 200) {
          res.resume();
          return reconnect();
        }
        retry = RETRY_MS;
        let buffered = "";
        res.setEncoding("utf8");
        res.on("data", (chunk) => {
          const split = sseData(buffered + chunk);
          buffered = split.rest;
          for (const data of split.data) {
            try {
              onFrame(JSON.parse(data));
            } catch {
              // A frame this process cannot read is one it must not act on.
            }
          }
        });
        res.on("end", reconnect);
        res.on("error", reconnect);
      },
    );
    request.on("error", reconnect);
  };

  let reconnecting = false;
  const reconnect = () => {
    if (stopped || reconnecting) return;
    reconnecting = true;
    onDrop();
    setTimeout(() => {
      reconnecting = false;
      connect();
    }, retry);
    retry = Math.min(retry * 2, MAX_RETRY_MS);
  };

  const flush = async () => {
    if (posting || queue.length === 0) return;
    posting = true;
    const frames = queue.splice(0, queue.length);
    try {
      await client.request("POST", "/browser-host/frames", frames);
    } catch {
      // The kernel is going away; what it did not take goes with it.
    }
    posting = false;
    flush();
  };

  connect();
  return {
    post(frame) {
      queue.push(frame);
      flush();
    },
    stop() {
      stopped = true;
      request?.destroy();
    },
  };
}

module.exports = { startBrowserRelay, sseData };
