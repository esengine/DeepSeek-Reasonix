"use strict";

// Where a link is allowed to go. Links come out of model output, so the platform
// opener must never be handed a scheme of its choosing — file:, and the handler
// registrations a machine happens to carry, are reached through exactly this.
function externalTarget(raw) {
  let url;
  try {
    url = new URL(String(raw));
  } catch {
    return null;
  }
  if (url.protocol !== "http:" && url.protocol !== "https:") return null;
  return url.toString();
}

module.exports = { externalTarget };
