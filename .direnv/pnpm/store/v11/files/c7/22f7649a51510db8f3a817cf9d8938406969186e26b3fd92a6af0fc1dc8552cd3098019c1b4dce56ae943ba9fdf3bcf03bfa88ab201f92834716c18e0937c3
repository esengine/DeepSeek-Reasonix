"use strict";
const { WebSocket } = require("undici");
const { parseURL, serializeURL, serializeURLOrigin } = require("whatwg-url");
const { setupForSimpleEventAccessors } = require("../helpers/create-event-accessor");
const { fireAnEvent } = require("../helpers/events");
const { copyToArrayBufferInTargetRealmDestructively } = require("../helpers/binary-data");
const IterableWeakSet = require("../helpers/iterable-weak-set");
const EventTargetImpl = require("../events/EventTarget-impl").implementation;
const Blob = require("../../../generated/idl/Blob");
const CloseEvent = require("../../../generated/idl/CloseEvent");
const DOMException = require("../../../generated/idl/DOMException");
const MessageEvent = require("../../../generated/idl/MessageEvent");

const openSockets = new WeakMap();

class WebSocketImpl extends EventTargetImpl {
  constructor(globalObject, [url, protocols]) {
    super(globalObject);

    // Do our own URL parsing because we want to be consistent with the rest of jsdom and use whatwg-url, not Node.js's
    // URL.
    const urlRecord = parseURL(url);
    if (urlRecord === null) {
      throw DOMException.create(this._globalObject, [`The URL '${url}' is invalid.`, "SyntaxError"]);
    }
    if (urlRecord.scheme !== "ws" && urlRecord.scheme !== "wss") {
      throw DOMException.create(this._globalObject, [
        `The URL's scheme must be either 'ws' or 'wss'. '${urlRecord.scheme}' is not allowed.`,
        "SyntaxError"
      ]);
    }
    if (urlRecord.fragment !== null) {
      throw DOMException.create(this._globalObject, [
        `The URL contains a fragment identifier ('${urlRecord.fragment}'). Fragment identifiers ` +
        "are not allowed in WebSocket URLs.",
        "SyntaxError"
      ]);
    }

    this._urlRecord = urlRecord;
    this._urlSerialized = serializeURL(urlRecord);
    this._binaryType = "blob";

    const wsOptions = {
      dispatcher: globalObject._dispatcher,
      protocols,
      headers: {
        // Origin is required for WebSocket and uses the window's origin
        origin: globalObject._origin
      }
    };

    this._ws = wrapAndRethrowNodeDOMExceptions(() => {
      return new WebSocket(serializeURL(urlRecord), wsOptions);
    }, this._globalObject);

    // Always use "arraybuffer" for `this._ws`'s `binaryType`. It will be converted to the correct type by `_onMessage`,
    // and jsdom's `Blob`s are just wrappers around `ArrayBuffer`s anyway.
    this._ws.binaryType = "arraybuffer";

    // Track open sockets for cleanup
    let openSocketsForWindow = openSockets.get(globalObject._globalProxy);
    if (openSocketsForWindow === undefined) {
      openSocketsForWindow = new IterableWeakSet();
      openSockets.set(globalObject._globalProxy, openSocketsForWindow);
    }
    openSocketsForWindow.add(this);

    // Set up event forwarding. We use `setTimeout()` to work around https://github.com/nodejs/undici/issues/4741 where
    // undici fires events synchronously during `close()`, but the spec requires them to fire asynchronously.
    this._ws.addEventListener("open", () => {
      setTimeout(() => fireAnEvent("open", this), 0);
    });

    this._ws.addEventListener("message", event => {
      // Capture readyState now, before setTimeout, because undici may transition to CLOSED before our setTimeout fires,
      // but the spec says readyState must be OPEN during message events.
      const readyStateWhenReceived = this._ws.readyState;
      setTimeout(() => {
        const prevReadyState = this._readyState;
        this._readyState = readyStateWhenReceived;
        this._onMessage(event);
        this._readyState = prevReadyState;
      }, 0);
    });

    this._ws.addEventListener("error", () => {
      setTimeout(() => fireAnEvent("error", this), 0);
    });

    this._ws.addEventListener("close", event => {
      setTimeout(() => {
        // Set readyState to CLOSED when firing the close event. We manage this ourselves because undici has bugs with
        // readyState during close: https://github.com/nodejs/undici/issues/4742.
        this._readyState = this._ws.CLOSED;
        openSocketsForWindow.delete(this);
        fireAnEvent("close", this, CloseEvent, {
          wasClean: event.wasClean,
          code: event.code,
          reason: event.reason
        });
      }, 0);
    });
  }

  _onMessage({ data }) {
    let dataForEvent;
    if (typeof data === "string") {
      dataForEvent = data;
    } else if (this._binaryType === "arraybuffer") {
      dataForEvent = copyToArrayBufferInTargetRealmDestructively(data, this._globalObject);
    } else {
      // `this._binaryType === "blob"`
      dataForEvent = Blob.create(this._globalObject, [undefined, { type: "" }], {
        fastPathArrayBufferToWrap: data
      });
    }

    fireAnEvent("message", this, MessageEvent, {
      data: dataForEvent,
      origin: serializeURLOrigin(this._urlRecord)
    });
  }

  get url() {
    return this._urlSerialized;
  }

  get readyState() {
    // Use captured readyState if available (workaround for undici bug #4742)
    return this._readyState ?? this._ws.readyState;
  }

  get bufferedAmount() {
    return this._ws.bufferedAmount;
  }

  get extensions() {
    return this._ws.extensions;
  }

  get protocol() {
    return this._ws.protocol;
  }

  get binaryType() {
    return this._binaryType;
  }

  set binaryType(value) {
    this._binaryType = value;
  }

  close(code, reason) {
    return wrapAndRethrowNodeDOMExceptions(() => {
      // Set readyState to CLOSING before calling undici's close(). We manage this ourselves because
      // undici has bugs with readyState during close - see https://github.com/nodejs/undici/issues/4742
      // Only set to CLOSING if not already CLOSED (calling close() on a closed socket is a no-op).
      if (this._readyState !== this._ws.CLOSED) {
        this._readyState = this._ws.CLOSING;
      }
      return this._ws.close(code, reason);
    }, this._globalObject);
  }

  send(data) {
    return wrapAndRethrowNodeDOMExceptions(() => {
      // Convert jsdom Blob to ArrayBuffer. Other types are passed through as-is.
      if (Blob.isImpl(data)) {
        data = data._bytes.buffer;
      }

      return this._ws.send(data);
    }, this._globalObject);
  }

  // https://websockets.spec.whatwg.org/#make-disappear
  // But with additional work from jsdom to remove all event listeners.
  _makeDisappear() {
    this._eventListeners = Object.create(null);
    if (this._ws.readyState === this._ws.OPEN || this._ws.readyState === this._ws.CONNECTING) {
      // Close without a code - undici doesn't allow reserved codes like 1001
      this._ws.close();
    }
  }

  static cleanUpWindow(window) {
    const openSocketsForWindow = openSockets.get(window._globalProxy);
    if (openSocketsForWindow !== undefined) {
      for (const ws of openSocketsForWindow) {
        ws._makeDisappear();
      }
      openSockets.delete(window._globalProxy);
    }
  }
}

function wrapAndRethrowNodeDOMExceptions(func, globalObject) {
  try {
    return func();
  } catch (e) {
    if (e instanceof globalThis.DOMException) {
      throw DOMException.create(globalObject, [e.message, e.name]);
    }
    throw e;
  }
}

setupForSimpleEventAccessors(WebSocketImpl.prototype, ["open", "message", "error", "close"]);

exports.implementation = WebSocketImpl;
