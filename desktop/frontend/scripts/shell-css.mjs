// The stylesheet marks OS drag regions once, with the --reasonix-draggable
// custom property. Chromium ignores var() for -webkit-app-region, so the
// Electron build rewrites the declaration at bundle time instead of carrying
// both forms in the source. The plain browser build keeps the inert marker.
const DRAG_PROPERTY = /--reasonix-draggable\s*:/g;

export function shellFromEnv(env = process.env) {
  const shell = (env.REASONIX_SHELL ?? "").trim().toLowerCase();
  if (shell === "") return "browser";
  if (shell === "electron") return "electron";
  throw new Error(`REASONIX_SHELL must be "electron" or unset, got ${JSON.stringify(shell)}`);
}

export function rewriteDragRegions(css, shell) {
  if (shell !== "electron") return css;
  return css.replace(DRAG_PROPERTY, "-webkit-app-region:");
}
