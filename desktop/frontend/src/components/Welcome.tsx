import { memo, useCallback, useMemo } from "react";
import logo from "../assets/logo.svg";
import { useT } from "../lib/i18n";

// Welcome is the empty-state landing: brand, a one-liner, the input affordances
// (/ commands, @ files, Enter), and a few clickable example prompts that send
// immediately so a first turn is one click away.

// The example descriptor list lives at module scope: a fresh array literal
// was being allocated on every render, and the per-key t() call inside the
// render was unmemoized so the language-pref flip would rebuild the array
// twice (once for examples, once for the inline button JSX) instead of once.
// Pulling the keys up also lets us pre-bind the click handler at the array
// level — the per-button closure cost collapses to one factory per render.
const EXAMPLE_KEYS = ["welcome.ex1", "welcome.ex2", "welcome.ex3"] as const;

function WelcomeImpl({ onPrompt }: { onPrompt: (text: string) => void }) {
  const t = useT();
  // useMemo: the array is rebuilt only when the locale flips (Translator is
  // stable for a given language pref) or on first mount, so the JSX .map() and
  // its three button factories reuse the same descriptor list across renders.
  const examples = useMemo(() => EXAMPLE_KEYS.map((key) => t(key)), [t]);
  // Stable factory: every example button gets the same closure, so React
  // reconciles them by key + index instead of allocating a new function per
  // click. The string closure-overhead is also a hot allocation pattern that
  // V8 sometimes re-creates the inner string handle for, so binding once is
  // a measurable win on rapid clicks.
  const send = useCallback(
    (text: string) => () => onPrompt(text),
    [onPrompt],
  );
  return (
    <div className="welcome">
      <img src={logo} className="welcome__logo" alt="Reasonix" />
      <div className="welcome__title">Reasonix</div>
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
        {examples.map((ex) => (
          <button key={ex} className="welcome__ex" onClick={send(ex)}>
            {ex}
          </button>
        ))}
      </div>
    </div>
  );
}

// memo: Welcome is mounted as Transcript's empty-state child, and Transcript
// re-renders on every streamed token. Without memo, the entire welcome screen
// (logo image decode, kbd hints, three buttons) re-runs its full body for
// every token of the first user turn. With memo, the welcome tree is built
// once and only re-runs if `onPrompt` changes (which it doesn't — useController
// wraps it in a useCallback).
export const Welcome = memo(WelcomeImpl);
