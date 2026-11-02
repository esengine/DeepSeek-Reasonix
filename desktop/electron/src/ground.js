"use strict";

// The page's own `--page` ground for each scheme (frontend-next tokens.css),
// as sRGB. A window paints this wherever the page does not, so a pixel the
// compositor leaves uncovered at a fractional scale matches the app.
const GROUND = { light: "#f4f1ea", dark: "#060a10" };

function groundFor(prefs, systemDark, painted) {
  const source = themeSourceFor(prefs, painted);
  const dark = source === "system" ? systemDark : source === "dark";
  return dark ? GROUND.dark : GROUND.light;
}

// The scheme the window frame takes. Windows draws a frameless window's border
// from it, so a dark page on a light system needs it set to stay borderless.
function themeSourceFor(prefs, painted) {
  const theme = painted?.mode ?? prefs?.["rx-theme"];
  if (theme === "scheduled") {
    const scheme = painted?.scheme ?? prefs?.["rx-theme-resolved"];
    return scheme === "dark" || scheme === "light" ? scheme : "system";
  }
  return theme === "dark" || theme === "light" ? theme : "system";
}

module.exports = { GROUND, groundFor, themeSourceFor };
