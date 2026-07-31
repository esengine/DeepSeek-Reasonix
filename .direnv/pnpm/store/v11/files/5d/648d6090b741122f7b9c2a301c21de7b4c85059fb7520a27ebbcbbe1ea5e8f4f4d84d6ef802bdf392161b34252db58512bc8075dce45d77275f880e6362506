"use strict";
const { Readable } = require("stream");
const { sendStreamResponse } = require("./stream-handler");

/**
 * Creates a user-friendly `undici` interceptor for jsdom.
 *
 * This helper allows users to intercept requests using a callback that receives a `Request` object and can return a
 * promise for a `Response` to provide a synthetic response, or a promise for `undefined` to pass through.
 *
 * @param {function} fn - Async callback function that receives (request, context) and can return a Response
 * @returns {function} An undici interceptor
 *
 * @example
 * const dom = new JSDOM(html, {
 *   interceptors: [
 *     requestInterceptor(async (request, { element }) => {
 *       console.log(`${element?.localName || 'XHR'} requested ${request.url}`);
 *       if (request.url.endsWith('/test.js')) {
 *         return new Response('window.mocked = true;', {
 *           headers: { 'Content-Type': 'application/javascript' }
 *         });
 *       }
 *       // Return undefined to let the request pass through
 *     })
 *   ]
 * });
 *
 * ## Why this doesn't use undici's DecoratorHandler pattern
 *
 * The standard undici interceptor pattern (see e.g. undici/lib/interceptor/dump.js) uses DecoratorHandler
 * to wrap handler callbacks. That pattern calls `dispatch()` synchronously, then observes/modifies the
 * request and response as they flow through the handler callbacks.
 *
 * This interceptor needs to support async user functions that can BLOCK the request and potentially
 * REPLACE it with a synthetic response. This requires:
 *
 * 1. Waiting for the async user function BEFORE deciding whether to dispatch
 * 2. For synthetic responses, NEVER calling dispatch() at all
 *
 * Since synthetic responses bypass the normal undici dispatch flow entirely, there's no underlying
 * dispatcher (Agent/Pool) to create a real controller for us. We provide our own controller object
 * that delegates to a Node.js Readable stream for pause/resume/abort support, following the pattern
 * from undici/lib/interceptor/cache.js.
 */
module.exports = function requestInterceptor(fn) {
  return dispatch => (options, handler) => {
    const { element = null, url } = options.opaque || {};
    const abortController = new AbortController();

    // Create undici controller and wrapped handler using the signal handler helper.
    // The undici controller reflects abortController.signal's state and forwards to inner undici controller.
    const { undiciController, wrappedHandler } = createSignalHandler(handler, abortController);

    // Call onRequestStart immediately to wire into the abort chain.
    handler.onRequestStart?.(undiciController, {});

    // Build Request object with our signal
    const requestInit = {
      method: options.method || "GET",
      headers: options.headers,
      signal: abortController.signal
    };
    if (options.body !== undefined && options.body !== null) {
      requestInit.body = options.body;
      requestInit.duplex = "half";
    }
    if (options.referrer) {
      requestInit.referrer = options.referrer;
    }
    const request = new Request(url, requestInit);

    new Promise(resolve => {
      resolve(fn(request, { element }));
    })
      .then(response => {
        if (response instanceof Response) {
          // Send synthetic response without ever starting real request
          // response.body can be null for responses with no body
          const stream = response.body ? Readable.fromWeb(response.body) : Readable.from([]);
          sendStreamResponse(wrappedHandler, stream, {
            status: response.status,
            statusText: response.statusText,
            headers: headersToUndici(response.headers)
          });
        } else if (response !== undefined) {
          throw new TypeError("requestInterceptor callback must return undefined or a Response");
        } else if (!abortController.signal.aborted) {
          // Pass through to real request
          dispatch(options, wrappedHandler);
        }
      })
      .catch(error => {
        handler.onResponseError?.(undiciController, error);
      });

    // Return true to indicate request is being handled
    return true;
  };
};

/**
 * Creates an undici controller and wrapped handler that bridge an AbortController to undici's dispatch protocol.
 *
 * The undici controller reflects the AbortController's signal state and captures an inner undici controller
 * (from pass-through dispatch or synthetic stream) for forwarding pause/resume/abort.
 */
function createSignalHandler(handler, abortController) {
  let innerUndiciController = null;

  const undiciController = {
    abort(reason) {
      abortController.abort(reason);
      innerUndiciController?.abort(reason);
    },
    pause() {
      innerUndiciController?.pause();
    },
    resume() {
      innerUndiciController?.resume();
    },
    get paused() {
      return innerUndiciController?.paused ?? false;
    },
    get aborted() {
      return abortController.signal.aborted;
    },
    get reason() {
      return abortController.signal.reason;
    }
  };

  const wrappedHandler = {
    onRequestStart(controller) {
      innerUndiciController = controller;
    },
    onRequestUpgrade(...args) {
      handler.onRequestUpgrade?.(...args);
    },
    onResponseStart(...args) {
      handler.onResponseStart?.(...args);
    },
    onResponseData(...args) {
      handler.onResponseData?.(...args);
    },
    onResponseEnd(...args) {
      handler.onResponseEnd?.(...args);
    },
    onResponseError(...args) {
      handler.onResponseError?.(...args);
    }
  };

  return { undiciController, wrappedHandler };
}

/**
 * Converts a Headers object to the format undici expects.
 * Handles multiple Set-Cookie headers via getSetCookie().
 */
function headersToUndici(headers) {
  const result = {};
  for (const [key, value] of headers) {
    result[key] = value;
  }
  const cookies = headers.getSetCookie();
  if (cookies.length > 0) {
    result["set-cookie"] = cookies;
  }
  return result;
}
