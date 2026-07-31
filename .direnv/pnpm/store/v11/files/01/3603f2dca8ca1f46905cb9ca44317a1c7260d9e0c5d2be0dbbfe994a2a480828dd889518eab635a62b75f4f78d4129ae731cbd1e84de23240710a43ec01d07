"use strict";

// Adapted from undici's lib/interceptor/decompress.js
// https://github.com/nodejs/undici/blob/main/lib/interceptor/decompress.js
// Changes:
// - Removed zstd support (requires runtimeFeatures check)
// - Removed experimental warning
// - Use undici's exported DecoratorHandler

const { createInflate, createGunzip, createBrotliDecompress } = require("zlib");
const { pipeline } = require("stream");
const { DecoratorHandler } = require("undici");

const supportedEncodings = {
  "gzip": createGunzip,
  "x-gzip": createGunzip,
  "br": createBrotliDecompress,
  "deflate": createInflate,
  "compress": createInflate,
  "x-compress": createInflate
};

const defaultSkipStatusCodes = [204, 304];

class DecompressHandler extends DecoratorHandler {
  #decompressors = [];
  #skipStatusCodes;
  #skipErrorResponses;

  constructor(handler, { skipStatusCodes = defaultSkipStatusCodes, skipErrorResponses = true } = {}) {
    super(handler);
    this.#skipStatusCodes = skipStatusCodes;
    this.#skipErrorResponses = skipErrorResponses;
  }

  #shouldSkipDecompression(contentEncoding, statusCode) {
    if (!contentEncoding || statusCode < 200) {
      return true;
    }
    if (this.#skipStatusCodes.includes(statusCode)) {
      return true;
    }
    if (this.#skipErrorResponses && statusCode >= 400) {
      return true;
    }
    return false;
  }

  #createDecompressionChain(encodings) {
    const parts = encodings.split(",");

    const maxContentEncodings = 5;
    if (parts.length > maxContentEncodings) {
      throw new Error(`too many content-encodings in response: ${parts.length}, max is ${maxContentEncodings}`);
    }

    const decompressors = [];

    for (let i = parts.length - 1; i >= 0; i--) {
      const encoding = parts[i].trim();
      if (!encoding) {
        continue;
      }

      if (!supportedEncodings[encoding]) {
        decompressors.length = 0;
        return decompressors;
      }

      decompressors.push(supportedEncodings[encoding]());
    }

    return decompressors;
  }

  #setupDecompressorEvents(decompressor, controller) {
    decompressor.on("readable", () => {
      let chunk;
      while ((chunk = decompressor.read()) !== null) {
        const result = super.onResponseData(controller, chunk);
        if (result === false) {
          break;
        }
      }
    });

    decompressor.on("error", error => {
      super.onResponseError(controller, error);
    });
  }

  #setupSingleDecompressor(controller) {
    const decompressor = this.#decompressors[0];
    this.#setupDecompressorEvents(decompressor, controller);

    decompressor.on("end", () => {
      super.onResponseEnd(controller, {});
    });
  }

  #setupMultipleDecompressors(controller) {
    const lastDecompressor = this.#decompressors[this.#decompressors.length - 1];
    this.#setupDecompressorEvents(lastDecompressor, controller);

    pipeline(this.#decompressors, err => {
      if (err) {
        super.onResponseError(controller, err);
        return;
      }
      super.onResponseEnd(controller, {});
    });
  }

  #cleanupDecompressors() {
    this.#decompressors.length = 0;
  }

  onResponseStart(controller, statusCode, headers, statusMessage) {
    const contentEncoding = headers["content-encoding"];

    if (this.#shouldSkipDecompression(contentEncoding, statusCode)) {
      return super.onResponseStart(controller, statusCode, headers, statusMessage);
    }

    const decompressors = this.#createDecompressionChain(contentEncoding.toLowerCase());

    if (decompressors.length === 0) {
      this.#cleanupDecompressors();
      return super.onResponseStart(controller, statusCode, headers, statusMessage);
    }

    this.#decompressors = decompressors;

    // Keep content-encoding and content-length headers as-is
    // XHR spec requires these to reflect the wire format, not the decoded body
    const newHeaders = { ...headers };

    if (this.#decompressors.length === 1) {
      this.#setupSingleDecompressor(controller);
    } else {
      this.#setupMultipleDecompressors(controller);
    }

    return super.onResponseStart(controller, statusCode, newHeaders, statusMessage);
  }

  onResponseData(controller, chunk) {
    if (this.#decompressors.length > 0) {
      this.#decompressors[0].write(chunk);
      return;
    }
    super.onResponseData(controller, chunk);
  }

  onResponseEnd(controller, trailers) {
    if (this.#decompressors.length > 0) {
      this.#decompressors[0].end();
      this.#cleanupDecompressors();
      return;
    }
    super.onResponseEnd(controller, trailers);
  }

  onResponseError(controller, err) {
    if (this.#decompressors.length > 0) {
      for (const decompressor of this.#decompressors) {
        decompressor.destroy(err);
      }
      this.#cleanupDecompressors();
    }
    super.onResponseError(controller, err);
  }
}

function createDecompressInterceptor(options = {}) {
  return dispatch => {
    return (opts, handler) => {
      const decompressHandler = new DecompressHandler(handler, options);
      return dispatch(opts, decompressHandler);
    };
  };
}

module.exports = createDecompressInterceptor;
