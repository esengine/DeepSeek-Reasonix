import "@fontsource/geist/400.css";
import "@fontsource/geist/500.css";
import "@fontsource/geist/600.css";
import "@fontsource/geist/700.css";
import "@fontsource/geist-mono/400.css";
import "@fontsource/geist-mono/500.css";
import "@fontsource/geist-mono/600.css";
import "@fontsource/inter/400.css";
import "@fontsource/inter/500.css";
import "@fontsource/inter/600.css";
import "@fontsource/inter/700.css";
import "katex/dist/katex.min.css";
import { createRoot } from "react-dom/client";
import { App } from "./App";
import { applyProductionLockdown } from "./prod-guard";
import { isTheme } from "./theme";

const stored = localStorage.getItem("reasonix.theme");
if (isTheme(stored)) {
  document.documentElement.dataset.theme = stored;
}

const platform = /Mac|macOS/i.test(navigator.userAgent)
  ? "macos"
  : /Windows/i.test(navigator.userAgent)
    ? "windows"
    : "default";
document.documentElement.dataset.platform = platform;
document.body.dataset.platform = platform;

applyProductionLockdown();

const host = document.getElementById("root");
if (!host) throw new Error("#root missing");

createRoot(host).render(<App />);
