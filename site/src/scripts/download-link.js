const DOWNLOAD_PANES = new Set(["npm", "brew", "desktop", "vscode"]);

// Return the requested install pane only for the homepage download section.
// Plain #start links keep the rendered default pane; query links opt into one.
export function downloadPaneFromURL(input, base = "https://reasonix.io/") {
  let url;
  try {
    url = new URL(input, base);
  } catch {
    return "";
  }
  if (url.hash !== "#start") return "";
  const pane = url.searchParams.get("download") || "";
  return DOWNLOAD_PANES.has(pane) ? pane : "";
}
