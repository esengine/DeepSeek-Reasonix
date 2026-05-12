import "@fontsource-variable/inter";
import "@fontsource-variable/jetbrains-mono";
import { createRoot } from "react-dom/client";
import { App } from "./App";

const stored = localStorage.getItem("reasonix.theme");
if (stored === "light" || stored === "dark") {
  document.documentElement.dataset.theme = stored;
}

const host = document.getElementById("root");
if (!host) throw new Error("#root missing");

createRoot(host).render(<App />);
