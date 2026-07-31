"use strict";

const HTMLElementImpl = require("./HTMLElement-impl").implementation;

const { asciiLowercase } = require("../helpers/strings");
const { closest } = require("../helpers/traversal");

class HTMLTableCellElementImpl extends HTMLElementImpl {
  get cellIndex() {
    const tr = closest(this, "tr");
    if (tr === null) {
      return -1;
    }

    return tr.cells.indexOf(this);
  }

  get scope() {
    let value = this.getAttributeNS(null, "scope");
    if (value === null) {
      return "";
    }

    // Enumerated attribute is matched ASCII-case-insensitively.
    value = asciiLowercase(value);
    if (value === "row" || value === "col" || value === "rowgroup" || value === "colgroup") {
      return value;
    }

    return "";
  }

  set scope(V) {
    this.setAttributeNS(null, "scope", V);
  }
}

module.exports = {
  implementation: HTMLTableCellElementImpl
};
