"use strict";

const DOMException = require("../../../generated/idl/DOMException.js");
const idlUtils = require("../../../generated/idl/utils.js");
const csstree = require("./helpers/patched-csstree.js");

// Parse a single medium string. Returns { normalized, canonical } or null.
// - normalized: lowercased original with whitespace collapsed (for storage/display)
// - canonical: csstree's serialization lowercased (for comparison)
//
// If csstree was spec-compliant, we would only need canonical. However, it is not:
// https://github.com/csstree/csstree/issues/52. This is a compromise, and still not fully spec-compliant.
function parseSingleMedium(text) {
  const trimmed = text.trim();
  if (trimmed === "") {
    return null;
  }
  try {
    const ast = csstree.parse("@media " + trimmed + " {}", { context: "stylesheet" });
    let mql = null;
    csstree.walk(ast, {
      visit: "MediaQueryList",
      enter(node) {
        mql = node;
      }
    });
    if (mql === null || mql.children.size !== 1) {
      return null;
    }
    const mq = mql.children.first;
    // csstree accepts <general-enclosed> tokens (e.g. "layer()") without error,
    // but per spec they make the media query invalid ("not all").
    if (mq.condition) {
      let hasGeneralEnclosed = false;
      csstree.walk(mq.condition, node => {
        if (node.type === "GeneralEnclosed") {
          hasGeneralEnclosed = true;
        }
      });
      if (hasGeneralEnclosed) {
        return null;
      }
    }
    return {
      normalized: trimmed.replace(/\s+/g, " ").toLowerCase(),
      canonical: csstree.generate(mq).toLowerCase()
    };
  } catch {
    return null;
  }
}

// Get the canonical (csstree-serialized) form of a stored medium, for comparison.
function canonicalize(stored) {
  const result = parseSingleMedium(stored);
  return result ? result.canonical : stored;
}

class MediaListImpl {
  constructor(globalObject, args, privateData) {
    this._globalObject = globalObject;
    this._list = [];

    if (privateData.mediaText) {
      this._parse(privateData.mediaText);
    }
  }

  get mediaText() {
    return this._list.join(", ");
  }

  set mediaText(value) {
    this._list.length = 0;
    if (value !== "") {
      this._parse(value);
    }
  }

  get length() {
    return this._list.length;
  }

  item(index) {
    return this._list[index] || null;
  }

  appendMedium(medium) {
    const result = parseSingleMedium(medium);
    const normalized = result !== null ? result.normalized : "not all";
    const canonical = result !== null ? result.canonical : "not all";
    for (const entry of this._list) {
      if (canonicalize(entry) === canonical) {
        return;
      }
    }
    this._list.push(normalized);
  }

  deleteMedium(medium) {
    const result = parseSingleMedium(medium);
    const canonical = result !== null ? result.canonical : "not all";
    const index = this._list.findIndex(entry => canonicalize(entry) === canonical);
    if (index === -1) {
      throw DOMException.create(this._globalObject, [
        `"${medium}" was not found in the media list.`,
        "NotFoundError"
      ]);
    }
    this._list.splice(index, 1);
  }

  get [idlUtils.supportedPropertyIndices]() {
    return this._list.keys();
  }

  _parse(mediaText) {
    for (const part of mediaText.split(",")) {
      const result = parseSingleMedium(part);
      this._list.push(result !== null ? result.normalized : "not all");
    }
  }
}

// Evaluate a media query list against jsdom's environment (screen media type).
// Returns true if any query in the list matches, or if the list is empty (= "all").
// Currently only handles plain media types; queries with media features don't match.
function evaluateMediaList(list) {
  if (list.length === 0) {
    return true;
  }
  for (const query of list) {
    if (query === "all" || query === "screen") {
      return true;
    }
  }
  return false;
}

exports.implementation = MediaListImpl;
exports.evaluateMediaList = evaluateMediaList;
