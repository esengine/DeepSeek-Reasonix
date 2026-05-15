// Lang context + t() helper. `t({zh, en})` returns the active-lang string.

window.REASONIX_VERSION = "0.43.0";

const LangCtx = React.createContext({ lang: "zh", setLang: () => {} });

function detectInitialLang() {
  try {
    const url = new URLSearchParams(location.search).get("lang");
    if (url === "en" || url === "zh") return url;
    const stored = localStorage.getItem("reasonix.lang");
    if (stored === "en" || stored === "zh") return stored;
    const nav = (navigator.language || "").toLowerCase();
    return nav.startsWith("zh") ? "zh" : "en";
  } catch {
    return "zh";
  }
}

function LangProvider({ children }) {
  const [lang, setLangState] = React.useState(detectInitialLang);
  const setLang = React.useCallback((v) => {
    setLangState(v);
    try {
      localStorage.setItem("reasonix.lang", v);
      const url = new URL(window.location.href);
      url.searchParams.set("lang", v);
      window.history.replaceState({}, "", url.toString());
    } catch {}
  }, []);
  React.useEffect(() => {
    document.documentElement.lang = lang === "en" ? "en" : "zh-CN";
  }, [lang]);
  const value = React.useMemo(() => ({ lang, setLang }), [lang, setLang]);
  return <LangCtx.Provider value={value}>{children}</LangCtx.Provider>;
}

function useLang() {
  return React.useContext(LangCtx);
}

// Resolve a bilingual string. Plain strings pass through unchanged.
function t(s, lang) {
  if (s == null) return "";
  if (typeof s === "string") return s;
  return s[lang] || s.zh || s.en || "";
}

window.LangCtx = LangCtx;
window.LangProvider = LangProvider;
window.useLang = useLang;
window.t = t;
