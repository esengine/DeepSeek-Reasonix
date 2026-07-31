"use strict";

const {
  isForbiddenRequestHeader,
  isForbiddenResponseHeaderName,
  isPrivilegedNoCORSRequestHeaderName,
  isNoCORSSafelistedRequestHeaderName,
  isNoCORSSafelistedRequestHeader
} = require("./header-types");
const { isHeaderName, isHeaderValue, normalizeHeaderValue } = require("./header-utils");
const HeaderList = require("./header-list");

function assertName(name) {
  if (!isHeaderName(name)) {
    throw new TypeError("name is invalid");
  }
}

function assertValue(value) {
  if (!isHeaderValue(value)) {
    throw new TypeError("value is invalid");
  }
}

class HeadersImpl {
  constructor(globalObject, args) {
    this.guard = "none";
    this.headerList = new HeaderList();

    if (args[0]) {
      this.#fill(args[0]);
    }
  }

  // https://fetch.spec.whatwg.org/#headers-validate
  #validate(name, value) {
    assertName(name);
    assertValue(value);

    switch (this.guard) {
      case "immutable": {
        throw new TypeError("Headers is immutable");
      }
      case "request": {
        if (isForbiddenRequestHeader(name, value)) {
          return false;
        }
        break;
      }
      case "response": {
        if (isForbiddenResponseHeaderName(name)) {
          return false;
        }
        break;
      }
    }

    return true;
  }

  // https://fetch.spec.whatwg.org/#concept-headers-fill
  #fill(init) {
    if (Array.isArray(init)) {
      for (const header of init) {
        if (header.length !== 2) {
          throw new TypeError("init is invalid");
        }
        this.append(header[0], header[1]);
      }
    } else {
      for (const [key, value] of Object.entries(init)) {
        this.append(key, value);
      }
    }
  }

  // https://fetch.spec.whatwg.org/#concept-headers-remove-privileged-no-cors-request-headers
  #removePrivilegedNoCORSHeaders() {
    this.headerList.delete("range");
  }

  // https://fetch.spec.whatwg.org/#dom-headers-append
  append(name, value) {
    value = normalizeHeaderValue(value);
    if (!this.#validate(name, value)) {
      return;
    }

    if (this.guard === "request-no-cors") {
      let temporaryValue = this.headerList.get(name);
      if (temporaryValue === null) {
        temporaryValue = value;
      } else {
        temporaryValue += ", " + value;
      }
      if (!isNoCORSSafelistedRequestHeader(name, temporaryValue)) {
        return;
      }
    }

    this.headerList.append(name, value);

    if (this.guard === "request-no-cors") {
      this.#removePrivilegedNoCORSHeaders();
    }
  }

  // https://fetch.spec.whatwg.org/#dom-headers-delete
  delete(name) {
    if (!this.#validate(name, "")) {
      return;
    }

    if (this.guard === "request-no-cors" &&
      !isNoCORSSafelistedRequestHeaderName(name) &&
      !isPrivilegedNoCORSRequestHeaderName(name)) {
      return;
    }

    if (!this.headerList.contains(name)) {
      return;
    }

    this.headerList.delete(name);

    if (this.guard === "request-no-cors") {
      this.#removePrivilegedNoCORSHeaders();
    }
  }

  // https://fetch.spec.whatwg.org/#dom-headers-get
  get(name) {
    assertName(name);
    return this.headerList.get(name);
  }

  // https://fetch.spec.whatwg.org/#dom-headers-getsetcookie
  getSetCookie() {
    return this.headerList.getAll("Set-Cookie") || [];
  }

  // https://fetch.spec.whatwg.org/#dom-headers-has
  has(name) {
    assertName(name);
    return this.headerList.contains(name);
  }

  // https://fetch.spec.whatwg.org/#dom-headers-set
  set(name, value) {
    value = normalizeHeaderValue(value);
    if (!this.#validate(name, value)) {
      return;
    }

    if (this.guard === "request-no-cors" && !isNoCORSSafelistedRequestHeader(name, value)) {
      return;
    }

    this.headerList.set(name, value);

    if (this.guard === "request-no-cors") {
      this.#removePrivilegedNoCORSHeaders();
    }
  }

  * [Symbol.iterator]() {
    for (const header of this.headerList.sortAndCombine()) {
      yield header;
    }
  }
}

exports.implementation = HeadersImpl;
