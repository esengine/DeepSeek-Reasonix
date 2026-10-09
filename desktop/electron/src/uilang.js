"use strict";

// The interface language the page will draw in, decided from the same two
// facts it uses: its saved choice (rx-lang) and, for anything but an explicit
// zh or en, the machine's first locale.
function isChinese(tag) {
  const s = String(tag ?? "").toLowerCase();
  return s.startsWith("zh") || s.startsWith("yue") || s.startsWith("cmn") || s.includes("hans") || s.includes("hant");
}

function uiLanguage(prefs, machineLocale) {
  const want = String(prefs?.["rx-lang"] ?? "").trim().toLowerCase();
  if (want === "zh" || want === "en") return want;
  return isChinese(machineLocale) ? "zh" : "en";
}

module.exports = { uiLanguage };
