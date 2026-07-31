"use strict";
// Persistent worker thread for handling synchronous XHR requests.
//
// This worker runs in a separate thread and performs async HTTP requests on behalf of the main
// thread, which blocks on Atomics.wait() until we signal completion. A single JSDOM instance is
// created at startup and reused across requests to avoid the ~400ms initialization cost each time.
//
// The signaling protocol uses a SharedArrayBuffer with two Int32 slots:
//   [0] = ack:  worker writes 1 here immediately upon receiving the message
//   [1] = done: worker writes 1 here after the response is ready
//
// The ack slot lets the main thread detect the race condition where the worker has exited (idle
// timeout) but the main thread hasn't processed the exit event yet. See sendSyncWorkerRequest()
// in XMLHttpRequest-impl.js for the main-thread side.

const { parentPort } = require("node:worker_threads");
const { JSDOM } = require("../../../..");
const idlUtils = require("../../../generated/idl/utils");

const dom = new JSDOM();
const IDLE_TIMEOUT_MS = 5000;

let idleTimer = setTimeout(() => process.exit(0), IDLE_TIMEOUT_MS);
idleTimer.unref();

parentPort.on("message", ({ sharedBuffer, responsePort, config }) => {
  clearTimeout(idleTimer);

  const int32 = new Int32Array(sharedBuffer);

  // Acknowledge receipt so the main thread knows we're alive.
  Atomics.store(int32, 0, 1);
  Atomics.notify(int32, 0);

  const xhr = new dom.window.XMLHttpRequest();
  const xhrImpl = idlUtils.implForWrapper(xhr);
  xhrImpl._adoptSerializedRequest(config);

  function done() {
    const response = xhrImpl._serializeResponse();
    const transfer = response.responseBytes ? [response.responseBytes.buffer] : [];
    responsePort.postMessage(response, transfer);
    Atomics.store(int32, 1, 1);
    Atomics.notify(int32, 1);

    idleTimer = setTimeout(() => process.exit(0), IDLE_TIMEOUT_MS);
    idleTimer.unref();
  }

  try {
    xhr.addEventListener("loadend", done, false);
    xhr.send(xhrImpl._body);
  } catch (error) {
    xhrImpl._error = error;
    done();
  }
});
