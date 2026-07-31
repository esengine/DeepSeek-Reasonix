"use strict";
const { mixin } = require("../../utils");
const HTMLElementImpl = require("./HTMLElement-impl").implementation;
const WindowEventHandlersImpl = require("./WindowEventHandlers-impl").implementation;

class HTMLFrameSetElementImpl extends HTMLElementImpl {}

mixin(HTMLFrameSetElementImpl.prototype, WindowEventHandlersImpl.prototype);

module.exports = {
  implementation: HTMLFrameSetElementImpl
};
