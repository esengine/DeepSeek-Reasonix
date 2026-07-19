// Reasonix — shared three-state theme switch (system / light / dark).
// The pre-paint inline script in each layout sets documentElement.dataset.theme
// before first paint; this module wires the toggle buttons and keeps the
// resolved theme in sync with OS changes while the preference is "system".
const THEME_KEY = "reasonix-theme";
const META_LIGHT = "#ffffff";
const META_DARK = "#1f232b";

export function currentThemePref() {
  try {
    const p = localStorage.getItem(THEME_KEY);
    return p === "light" || p === "dark" ? p : "system";
  } catch (e) {
    return "system";
  }
}

export function resolveTheme(pref) {
  return pref === "dark" ||
    (pref === "system" && window.matchMedia("(prefers-color-scheme: dark)").matches)
    ? "dark"
    : "light";
}

export function applyTheme(pref) {
  const resolved = resolveTheme(pref);
  document.documentElement.dataset.theme = resolved;
  const meta = document.querySelector('meta[name="theme-color"]');
  if (meta) meta.content = resolved === "dark" ? META_DARK : META_LIGHT;
  document.querySelectorAll(".theme-switch button").forEach((b) => {
    const on = b.dataset.theme === pref;
    b.classList.toggle("active", on);
    if (on) b.setAttribute("aria-pressed", "true");
    else b.setAttribute("aria-pressed", "false");
  });
}

export function initTheme() {
  applyTheme(currentThemePref());
  const mq = window.matchMedia("(prefers-color-scheme: dark)");
  const onOsChange = () => {
    const pref = currentThemePref();
    if (pref === "system") applyTheme(pref);
  };
  if (mq.addEventListener) mq.addEventListener("change", onOsChange);
  document.querySelectorAll(".theme-switch button").forEach((b) => {
    b.addEventListener("click", () => {
      try {
        localStorage.setItem(THEME_KEY, b.dataset.theme);
      } catch (e) {}
      applyTheme(b.dataset.theme);
    });
  });
}
