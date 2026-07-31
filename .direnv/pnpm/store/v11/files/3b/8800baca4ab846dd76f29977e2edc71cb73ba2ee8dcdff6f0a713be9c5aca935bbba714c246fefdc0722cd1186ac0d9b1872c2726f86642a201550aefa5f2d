"use strict";
const PageTransitionEvent = require("../../../generated/idl/PageTransitionEvent.js");
const { fireAnEvent } = require("./events");

// https://html.spec.whatwg.org/multipage/browsing-the-web.html#fire-a-page-transition-event
exports.fireAPageTransitionEvent = (eventName, window, persisted) => {
  const attributes = { persisted, cancelable: true, bubbles: true };
  fireAnEvent(eventName, window, PageTransitionEvent, attributes, true);
};
