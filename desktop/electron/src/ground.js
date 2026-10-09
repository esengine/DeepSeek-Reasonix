"use strict";

// The page's own `--page` ground for each scheme (frontend-next tokens.css),
// as sRGB. A window paints this wherever the page does not, so a pixel the
// compositor leaves uncovered at a fractional scale matches the app.
const GROUND = { light: "#f4f1ea", dark: "#060a10" };

// "auto" and anything unrecognised follow the system, as the page does.
function groundFor(prefs, systemDark) {
  const theme = prefs?.["rx-theme"];
  const dark = theme === "dark" ? true : theme === "light" ? false : systemDark;
  return dark ? GROUND.dark : GROUND.light;
}

module.exports = { GROUND, groundFor };
