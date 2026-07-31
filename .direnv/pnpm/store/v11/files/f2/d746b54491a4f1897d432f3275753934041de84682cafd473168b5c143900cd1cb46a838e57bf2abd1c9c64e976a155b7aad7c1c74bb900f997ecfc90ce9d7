"use strict";

const { createEventAccessor } = require("../helpers/create-event-accessor");
const { windowEventHandlersEvents } = require("../../../generated/event-sets");

class WindowEventHandlersImpl {}

for (const event of windowEventHandlersEvents) {
  createEventAccessor(WindowEventHandlersImpl.prototype, event);
}

module.exports = {
  implementation: WindowEventHandlersImpl
};
