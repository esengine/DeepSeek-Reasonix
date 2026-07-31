"use strict";

const { MIMEType } = require("whatwg-mimetype");

// https://fetch.spec.whatwg.org/#privileged-no-cors-request-header-name
const PRIVILEGED_NO_CORS_REQUEST = new Set(["range"]);
function isPrivilegedNoCORSRequestHeaderName(name) {
  return PRIVILEGED_NO_CORS_REQUEST.has(name.toLowerCase());
}

// https://fetch.spec.whatwg.org/#no-cors-safelisted-request-header-name
const NO_CORS_SAFELISTED_REQUEST = new Set([
  `accept`,
  `accept-language`,
  `content-language`,
  `content-type`
]);
function isNoCORSSafelistedRequestHeaderName(name) {
  return NO_CORS_SAFELISTED_REQUEST.has(name.toLowerCase());
}

// https://fetch.spec.whatwg.org/#forbidden-response-header-name
const FORBIDDEN_RESPONSE = new Set(["set-cookie", "set-cookie2"]);
function isForbiddenResponseHeaderName(name) {
  return FORBIDDEN_RESPONSE.has(name.toLowerCase());
}

// https://fetch.spec.whatwg.org/#cors-safelisted-request-header
// Note: name and value are already ensured by the IDL layer to be byte strings.
const CORS_UNSAFE_BYTE = /[\x00-\x08\x0A-\x1F"():<>?@[\\\]{}\x7F]/;
function isCORSSafelistedRequestHeader(name, value) {
  name = name.toLowerCase();
  switch (name) {
    case "accept":
      if (value.match(CORS_UNSAFE_BYTE)) {
        return false;
      }
      break;
    case "accept-language":
    case "content-language":
      if (value.match(/[^\x30-\x39\x41-\x5A\x61-\x7A *,\-.;=]/)) {
        return false;
      }
      break;
    case "content-type": {
      if (value.match(CORS_UNSAFE_BYTE)) {
        return false;
      }
      const mimeType = MIMEType.parse(value);
      if (mimeType === null) {
        return false;
      }
      if (
        ![
          "application/x-www-form-urlencoded",
          "multipart/form-data",
          "text/plain"
        ].includes(mimeType.essence)
      ) {
        return false;
      }
      break;
    }
    default:
      return false;
  }
  if (value.length > 128) {
    return false;
  }
  return true;
}

// https://fetch.spec.whatwg.org/#no-cors-safelisted-request-header
function isNoCORSSafelistedRequestHeader(name, value) {
  if (!isNoCORSSafelistedRequestHeaderName(name)) {
    return false;
  }
  return isCORSSafelistedRequestHeader(name, value);
}

const BASIC_FORBIDDEN_REQUEST_HEADERS = new Set([
  "accept-charset",
  "accept-encoding",
  "access-control-request-headers",
  "access-control-request-method",
  "connection",
  "content-length",
  "cookie",
  "cookie2",
  "date",
  "dnt",
  "expect",
  "host",
  "keep-alive",
  "origin",
  "referer",
  "te",
  "trailer",
  "transfer-encoding",
  "upgrade",
  "via"
]);

const METHOD_CHECKING_FORBIDDEN_REQUEST_HEADERS = new Set([
  "x-http-method",
  "x-http-method-override",
  "x-method-override"
]);

const FORBIDDEN_METHODS = new Set([
  "connect",
  "trace",
  "track"
]);

// https://fetch.spec.whatwg.org/#forbidden-method
function isForbiddenMethod(value) {
  return FORBIDDEN_METHODS.has(value.toLowerCase());
}

// https://fetch.spec.whatwg.org/#forbidden-request-header
function isForbiddenRequestHeader(name, value) {
  const lowercaseName = name.toLowerCase();
  if (BASIC_FORBIDDEN_REQUEST_HEADERS.has(lowercaseName)) {
    return true;
  }
  if (lowercaseName.startsWith("proxy-") || lowercaseName.startsWith("sec-")) {
    return true;
  }

  if (METHOD_CHECKING_FORBIDDEN_REQUEST_HEADERS.has(lowercaseName)) {
    const parsedValues = getDecodeAndSplit(value);
    return parsedValues.some(isForbiddenMethod);
  }

  return false;
}

// https://fetch.spec.whatwg.org/#header-value-get-decode-and-split
function getDecodeAndSplit(input) {
  const values = [];
  let temporaryValue = "";
  let position = 0;

  while (true) {
    // Collect sequence of code points that are not " or ,
    while (position < input.length && input[position] !== "\"" && input[position] !== ",") {
      temporaryValue += input[position++];
    }

    // If position is not past end and code point is "
    if (position < input.length && input[position] === '"') {
      // Inlined: collect HTTP quoted string (extract-value = false)
      const positionStart = position++;
      while (true) {
        while (position < input.length && input[position] !== "\"" && input[position] !== "\\") {
          position++;
        }
        if (position >= input.length) {
          break;
        }
        if (input[position++] === "\\") {
          if (position >= input.length) {
            break;
          }
          position++;
        } else {
          break; // It was "
        }
      }
      temporaryValue += input.slice(positionStart, position);
      if (position < input.length) {
        continue;
      }
    }

    // Remove HTTP tab or space from start and end
    let start = 0;
    let end = temporaryValue.length;
    while (start < end && (temporaryValue[start] === "\t" || temporaryValue[start] === " ")) {
      start++;
    }
    while (end > start && (temporaryValue[end - 1] === "\t" || temporaryValue[end - 1] === " ")) {
      end--;
    }

    values.push(temporaryValue.slice(start, end));
    temporaryValue = "";

    if (position >= input.length) {
      return values;
    }
    // Assert: code point at position is ,
    position++;
  }
}

module.exports = {
  isPrivilegedNoCORSRequestHeaderName,
  isNoCORSSafelistedRequestHeaderName,
  isNoCORSSafelistedRequestHeader,
  isForbiddenRequestHeader,
  isForbiddenResponseHeaderName,
  isCORSSafelistedRequestHeader
};
