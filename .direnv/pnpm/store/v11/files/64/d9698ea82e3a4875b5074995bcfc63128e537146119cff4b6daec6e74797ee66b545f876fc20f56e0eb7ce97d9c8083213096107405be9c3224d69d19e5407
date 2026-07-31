"use strict";

const AbortSignal = require("../../../generated/idl/AbortSignal");

class AbortControllerImpl {
  constructor(globalObject) {
    this.signal = AbortSignal.createImpl(globalObject, []);
  }

  abort(reason) {
    this.signal._signalAbort(reason);
  }
}

module.exports = {
  implementation: AbortControllerImpl
};
