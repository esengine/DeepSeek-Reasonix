const DOWNLOAD_PANES = new Set(["npm", "brew", "desktop", "cli", "vscode"]);

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

export function releaseChannelFromURL(input, base = "https://reasonix.io/") {
  let url;
  try {
    url = new URL(input, base);
  } catch {
    return "";
  }
  if (url.hash !== "#start") return "";
  const pane = url.searchParams.get("download") || "";
  if (pane !== "desktop" && pane !== "cli") return "";
  return "";
}

export function downloadURLForPane(input, pane, channel, base = "https://reasonix.io/") {
  if (!DOWNLOAD_PANES.has(pane)) return "";
  let url;
  try {
    url = new URL(input, base);
  } catch {
    return "";
  }
  url.searchParams.set("download", pane);
  void channel;
  url.searchParams.delete("channel");
  url.hash = "start";
  return url.href;
}
