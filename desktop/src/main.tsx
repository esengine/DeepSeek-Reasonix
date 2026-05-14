import "@fontsource/ibm-plex-sans/400.css";
import "@fontsource/ibm-plex-sans/500.css";
import "@fontsource/ibm-plex-sans/600.css";
import "@fontsource/ibm-plex-sans/700.css";
import "@fontsource/ibm-plex-mono/400.css";
import "@fontsource/ibm-plex-mono/500.css";
import "@fontsource/ibm-plex-mono/600.css";
import "@fontsource/ibm-plex-serif/400.css";
import "@fontsource/ibm-plex-serif/500.css";
import { createRoot } from "react-dom/client";
import { App } from "./App";

const stored = localStorage.getItem("reasonix.theme");
if (stored === "light" || stored === "dark") {
  document.documentElement.dataset.theme = stored;
}

const host = document.getElementById("root");
if (!host) throw new Error("#root missing");

createRoot(host).render(<App />);
