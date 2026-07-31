"use strict";

/**
 * Sends a response to an undici handler by streaming data from a Node.js Readable stream.
 *
 * ## Why this exists
 *
 * undici's new handler API (onRequestStart, onResponseStart, onResponseData, onResponseEnd, onResponseError)
 * requires a "controller" object with pause/resume/abort methods. When responses flow through undici's
 * normal network stack, undici provides this controller. But when we generate responses ourselves
 * (e.g., for file: URLs, data: URLs, or synthetic responses from interceptors), we need to provide
 * our own controller.
 *
 * This helper creates a controller backed by a Node.js Readable stream, giving callers proper
 * pause/resume and abort support. It follows the pattern from undici/lib/interceptor/cache.js.
 *
 * ## Usage
 *
 * ```javascript
 * const stream = fs.createReadStream(filePath);
 * // or: const stream = Readable.from(buffer);
 * // or: const stream = Readable.fromWeb(response.body);
 *
 * sendStreamResponse(handler, stream, {
 *   status: 200,
 *   statusText: "OK",
 *   headers: { "content-type": "text/plain" },
 *   context: { history: ["file:///path/to/file"] }
 * });
 * ```
 *
 * The function handles all stream events and translates them to handler callbacks.
 * If the stream is destroyed (e.g., via controller.abort()), pending operations are cancelled.
 *
 * @param {object} handler - undici handler with new API methods (onRequestStart, onResponseStart, etc.)
 * @param {Readable} stream - Node.js Readable stream containing the response body
 * @param {object} options - Response metadata
 * @param {number} options.status - HTTP status code
 * @param {string} [options.statusText] - HTTP status text (default: "")
 * @param {object} [options.headers] - Response headers as {name: value} object (default: {})
 * @param {object} [options.context] - Context object passed to onRequestStart (default: {})
 */
function sendStreamResponse(handler, stream, { status, statusText = "", headers = {}, context = {} }) {
  const controller = {
    resume() {
      stream.resume();
    },
    pause() {
      stream.pause();
    },
    get paused() {
      return stream.isPaused();
    },
    get aborted() {
      return stream.errored !== null;
    },
    get reason() {
      return stream.errored;
    },
    abort(reason) {
      stream.destroy(reason);
    }
  };

  stream
    .on("error", err => {
      if (!stream.readableEnded) {
        handler.onResponseError?.(controller, err);
      }
    })
    .on("close", () => {
      if (!stream.errored) {
        handler.onResponseEnd?.(controller, {});
      }
    })
    .on("data", chunk => {
      handler.onResponseData?.(controller, chunk);
    });

  handler.onRequestStart?.(controller, context);

  if (stream.destroyed) {
    return;
  }

  handler.onResponseStart?.(controller, status, headers, statusText);
}

module.exports = { sendStreamResponse };
