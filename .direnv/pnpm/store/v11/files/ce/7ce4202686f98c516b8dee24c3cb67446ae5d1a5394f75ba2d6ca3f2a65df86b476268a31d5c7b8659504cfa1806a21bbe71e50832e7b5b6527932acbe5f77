"use strict";

const MediaList = require("../../../generated/idl/MediaList.js");

class StyleSheetImpl {
  constructor(globalObject, args, privateData = {}) {
    this._globalObject = globalObject;

    this.type = "text/css";
    this.href = privateData.href || null;
    this.title = privateData.title || null;
    this.disabled = false;

    this.ownerNode = privateData.ownerNode || null;
    this.parentStyleSheet = privateData.parentStyleSheet || null;
    this.media = MediaList.createImpl(globalObject, [], {
      mediaText: privateData.mediaText || ""
    });
  }
}

exports.implementation = StyleSheetImpl;
