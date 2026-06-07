import { useEffect, useRef, useState } from "react";
import logoWordmark from "../assets/logo-wordmark.svg";
import { app } from "../lib/bridge";
import { useT } from "../lib/i18n";

// Welcome is the empty-state landing: brand, a one-liner, the input affordances
// (/ commands, @ files, Enter), and a few clickable example prompts.
//
// The prompts are generated from the workspace content: the component calls
// ListDir("") on mount, inspects top-level file/folder names, and produces
// three context-aware suggestions.  While the fetch is in flight, the fixed
// i18n examples are shown so the user never sees a blank state.

function generatePrompts(names: string[]): string[] {
  const set = new Set(names);
  const has = (f: string) => set.has(f);

  // Detect project kind from root-level files.
  const isJS = has("package.json");
  const isGo = has("go.mod");
  const isRust = has("Cargo.toml");
  const isPython = has("pyproject.toml") || has("setup.py") || has("requirements.txt");
  const hasDocker = has("Dockerfile") || has("docker-compose.yml") || has("docker-compose.yaml");
  const hasCI = has(".github") || has(".gitlab-ci.yml") || has(".circleci");
  const hasTests = has("test") || has("tests") || has("__tests__") || has("spec");
  const hasDocs = has("docs") || has("doc") || has("README.md");

  // Build file-type description for the "architecture" prompt.
  let fileHint = "";
  if (isJS) fileHint = " (package.json, tsconfig, src/)";
  else if (isGo) fileHint = " (go.mod, cmd/, internal/)";
  else if (isRust) fileHint = " (Cargo.toml, src/)";
  else if (isPython) fileHint = " (pyproject.toml, src/)";

  // --- Prompt 1: architecture / overview (always present) ---
  const p1 = `Explain this codebase's architecture${fileHint}`;

  // --- Prompt 2: pick the most useful "second question" ---
  let p2: string;
  if (hasDocker) {
    p2 = "Explain the Docker setup and how the services are connected";
  } else if (hasCI) {
    p2 = "Summarise the CI/CD pipeline and what each step does";
  } else if (hasTests) {
    p2 = "How is the test suite structured? What testing frameworks are used?";
  } else {
    p2 = "Summarise the recent git changes";
  }

  // --- Prompt 3: dive into a specific area ---
  let p3: string;
  if (isJS && has("src")) {
    p3 = "Walk me through the entry point and main module structure in src/";
  } else if (isGo && (has("cmd") || has("internal"))) {
    p3 = "What are the main packages and how do they depend on each other?";
  } else if (isRust && has("src")) {
    p3 = "Explain the crate structure — what does each module in src/ do?";
  } else if (isPython && has("src")) {
    p3 = "Explain the package layout and the main entry point";
  } else if (hasDocs) {
    p3 = "What does the README say about getting started and contributing?";
  } else {
    p3 = "Where is the agent run loop, and what does it do?";
  }

  return [p1, p2, p3];
}

export function Welcome({ onPrompt }: { onPrompt: (text: string) => void }) {
  const t = useT();
  // Start with the fixed i18n examples — always available, zero latency.
  const fallback = [t("welcome.ex1"), t("welcome.ex2"), t("welcome.ex3")];
  const [prompts, setPrompts] = useState<string[]>(fallback);
  const fetchedRef = useRef(false);

  useEffect(() => {
    if (fetchedRef.current) return;
    fetchedRef.current = true;

    // Ask the backend for the workspace root listing.  ListDir("") is fast
    // (one readdir) and the result is tiny (~20 entries).  If it fails we
    // silently keep the fallback prompts.
    app
      .ListDir("")
      .then((entries) => {
        if (!entries || entries.length === 0) return;
        const names = entries.map((e) => e.name);
        const generated = generatePrompts(names);
        setPrompts(generated);
      })
      .catch(() => {});
  }, []);

  return (
    <div className="welcome">
      <img src={logoWordmark} className="welcome__logo" alt="Reasonix" />
      <div className="welcome__tag">{t("welcome.tagline")}</div>

      <div className="welcome__hints">
        <span>
          <kbd>/</kbd> {t("welcome.hintCommands")}
        </span>
        <span>
          <kbd>@</kbd> {t("welcome.hintFiles")}
        </span>
        <span>
          <kbd>⏎</kbd> {t("welcome.hintSend")}
        </span>
      </div>

      <div className="welcome__examples">
        {prompts.map((ex) => (
          <button key={ex} className="welcome__ex" onClick={() => onPrompt(ex)}>
            {ex}
          </button>
        ))}
      </div>
    </div>
  );
}
